import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { authFilesApi } from '@/services/api/authFiles';
import { usageApi, type UsageEventItem, type UsageEventsQuery } from '@/services/api';
import type { AuthFileItem, Config } from '@/types';
import type { CredentialInfo, SourceInfo } from '@/types/sourceInfo';
import { buildSourceInfoMap, resolveSourceDisplay } from '@/utils/sourceResolver';
import { normalizeAuthIndex, type UsageDetailWithEndpoint } from '@/utils/usage';
import type { ParsedLogLine } from './logTypes';

export type TraceCandidate = {
  detail: UsageDetailWithEndpoint;
  modelMatched: boolean;
  timeDeltaMs: number | null;
};

const TRACE_AUTH_CACHE_MS = 60 * 1000;
const TRACE_MAX_CANDIDATES = 5;
const TRACE_EVENTS_PAGE_SIZE = 200;
const TRACE_LOOKUP_WINDOW_MS = 2 * 60 * 60 * 1000;

const TRACEABLE_EXACT_PATHS = new Set(['/v1/chat/completions', '/v1/messages', '/v1/responses']);
const TRACEABLE_PREFIX_PATHS = ['/v1beta/models'];

const normalizeTracePath = (value?: string) =>
  String(value ?? '')
    .replace(/^"+|"+$/g, '')
    .split('?')[0]
    .trim();

const normalizeTraceablePath = (value?: string): string => {
  const normalized = normalizeTracePath(value);
  if (!normalized || normalized === '/') return normalized;
  return normalized.replace(/\/+$/, '');
};

const normalizeTraceMethod = (value?: string) =>
  String(value ?? '')
    .trim()
    .toUpperCase();

export const isTraceableRequestPath = (value?: string): boolean => {
  const normalizedPath = normalizeTraceablePath(value);
  if (!normalizedPath) return false;
  if (TRACEABLE_EXACT_PATHS.has(normalizedPath)) return true;
  return TRACEABLE_PREFIX_PATHS.some((prefix) => normalizedPath.startsWith(prefix));
};

const MODEL_EXTRACT_REGEX = /\bmodel[=:]\s*"?([a-zA-Z0-9._\-/]+)"?/i;

const extractModelFromMessage = (message?: string): string | undefined => {
  if (!message) return undefined;
  const match = message.match(MODEL_EXTRACT_REGEX);
  return match?.[1] || undefined;
};

const isPathMatch = (logPath: string, detailPath: string): boolean => {
  if (!logPath || !detailPath) return false;
  return logPath === detailPath || logPath.startsWith(detailPath) || detailPath.startsWith(logPath);
};

const buildTraceEndpointLabel = (
  requestMethod: string,
  requestPath: string,
  fallback: string
): string => {
  const normalizedMethod = normalizeTraceMethod(requestMethod);
  const normalizedPath = normalizeTracePath(requestPath);
  if (normalizedPath) {
    return normalizedMethod ? `${normalizedMethod} ${normalizedPath}` : normalizedPath;
  }
  return fallback.trim() || 'unknown';
};

const buildTraceEventsQuery = (line: ParsedLogLine): UsageEventsQuery | null => {
  const requestPath = normalizeTracePath(line.path);
  if (!requestPath) return null;

  const timestampMs = line.timestamp ? Date.parse(line.timestamp) : Number.NaN;
  const query: UsageEventsQuery = {
    page: 1,
    pageSize: TRACE_EVENTS_PAGE_SIZE,
    requestMethod: normalizeTraceMethod(line.method),
    requestPath,
  };

  if (!Number.isNaN(timestampMs)) {
    query.start = new Date(timestampMs - TRACE_LOOKUP_WINDOW_MS).toISOString();
    query.end = new Date(timestampMs + TRACE_LOOKUP_WINDOW_MS).toISOString();
  }

  return query;
};

const adaptTraceUsageDetails = (items: UsageEventItem[]): UsageDetailWithEndpoint[] =>
  items.map((item) => {
    const endpointLabel = buildTraceEndpointLabel(
      item.requestMethod,
      item.requestPath,
      item.apiKey || item.provider
    );
    const timestampMs = Date.parse(item.timestamp);
    return {
      timestamp: item.timestamp,
      source: item.source,
      auth_index: (item.authIndex || '') as unknown as number,
      tokens: {
        input_tokens: item.tokens.inputTokens,
        output_tokens: item.tokens.outputTokens,
        reasoning_tokens: item.tokens.reasoningTokens,
        cached_tokens: item.tokens.cachedTokens,
        total_tokens: item.tokens.totalTokens,
      },
      failed: item.failed,
      __modelName: item.model,
      __endpoint: endpointLabel,
      __endpointMethod: item.requestMethod || undefined,
      __endpointPath: normalizeTracePath(item.requestPath) || undefined,
      __timestampMs: Number.isNaN(timestampMs) ? 0 : timestampMs,
    };
  });

const getErrorMessage = (err: unknown): string => {
  if (err instanceof Error) return err.message;
  if (typeof err === 'string') return err;
  if (typeof err !== 'object' || err === null) return '';
  if (!('message' in err)) return '';

  const message = (err as { message?: unknown }).message;
  return typeof message === 'string' ? message : '';
};

interface UseTraceResolverOptions {
  traceScopeKey: string;
  connectionStatus: string;
  config: Config | null;
  requestLogDownloading: boolean;
}

interface UseTraceResolverReturn {
  traceLogLine: ParsedLogLine | null;
  traceLoading: boolean;
  traceError: string;
  traceCandidates: TraceCandidate[];
  resolveTraceSourceInfo: (sourceRaw: string, authIndex: unknown) => SourceInfo;
  loadTraceUsageDetails: () => Promise<void>;
  refreshTraceUsageDetails: () => Promise<void>;
  openTraceModal: (line: ParsedLogLine) => void;
  closeTraceModal: () => void;
}

export function useTraceResolver(options: UseTraceResolverOptions): UseTraceResolverReturn {
  const { traceScopeKey, connectionStatus, config, requestLogDownloading } = options;
  const { t } = useTranslation();

  const [traceLogLine, setTraceLogLine] = useState<ParsedLogLine | null>(null);
  const [traceUsageDetails, setTraceUsageDetails] = useState<UsageDetailWithEndpoint[]>([]);
  const [traceAuthFileMap, setTraceAuthFileMap] = useState<Map<string, CredentialInfo>>(new Map());
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceError, setTraceError] = useState('');

  const traceAuthLoadedAtRef = useRef(0);
  const traceScopeKeyRef = useRef('');
  const traceLogLineRef = useRef<ParsedLogLine | null>(null);

  const traceSourceInfoMap = useMemo(() => buildSourceInfoMap(config ?? {}), [config]);

  const loadTraceUsageDetailsInternal = useCallback(
    async (_forceUsage: boolean, lineOverride?: ParsedLogLine | null) => {
      if (traceScopeKeyRef.current !== traceScopeKey) {
        traceScopeKeyRef.current = traceScopeKey;
        traceAuthLoadedAtRef.current = 0;
        traceLogLineRef.current = null;
        setTraceUsageDetails([]);
        setTraceAuthFileMap(new Map());
        setTraceError('');
      }

      if (traceLoading) return;
      const activeLine = lineOverride ?? traceLogLineRef.current;
      const eventsQuery = activeLine ? buildTraceEventsQuery(activeLine) : null;
      if (!eventsQuery) {
        setTraceUsageDetails([]);
        return;
      }

      const now = Date.now();
      const authFresh =
        traceAuthLoadedAtRef.current > 0 &&
        now - traceAuthLoadedAtRef.current < TRACE_AUTH_CACHE_MS;

      setTraceLoading(true);
      setTraceError('');
      setTraceUsageDetails([]);
      try {
        const [eventsResponse, authFilesResponse] = await Promise.all([
          usageApi.getUsageEvents(eventsQuery),
          authFresh ? Promise.resolve(null) : authFilesApi.list().catch(() => null),
        ]);
        setTraceUsageDetails(adaptTraceUsageDetails(eventsResponse.items));

        if (authFilesResponse !== null) {
          const files = Array.isArray(authFilesResponse)
            ? authFilesResponse
            : (authFilesResponse as { files?: AuthFileItem[] })?.files;
          if (Array.isArray(files)) {
            const map = new Map<string, CredentialInfo>();
            files.forEach((file) => {
              const key = normalizeAuthIndex(file['auth_index'] ?? file.authIndex);
              if (!key) return;
              map.set(key, {
                name: file.name || key,
                type: (file.type || file.provider || '').toString(),
              });
            });
            setTraceAuthFileMap(map);
            traceAuthLoadedAtRef.current = Date.now();
          }
        }
      } catch (err: unknown) {
        setTraceError(getErrorMessage(err) || t('logs.trace_usage_load_error'));
      } finally {
        setTraceLoading(false);
      }
    },
    [t, traceLoading, traceScopeKey]
  );

  const loadTraceUsageDetails = useCallback(async () => {
    await loadTraceUsageDetailsInternal(false);
  }, [loadTraceUsageDetailsInternal]);

  const refreshTraceUsageDetails = useCallback(async () => {
    await loadTraceUsageDetailsInternal(true);
  }, [loadTraceUsageDetailsInternal]);

  useEffect(() => {
    if (connectionStatus === 'connected') {
      traceScopeKeyRef.current = traceScopeKey;
      traceAuthLoadedAtRef.current = 0;
      traceLogLineRef.current = null;
      setTraceUsageDetails([]);
      setTraceAuthFileMap(new Map());
      setTraceLoading(false);
      setTraceError('');
    }
  }, [connectionStatus, traceScopeKey]);

  const traceCandidates = useMemo(() => {
    if (!traceLogLine) return [];

    const logPath = normalizeTracePath(traceLogLine.path);
    if (!logPath) return [];

    const logTimestampMs = traceLogLine.timestamp ? Date.parse(traceLogLine.timestamp) : Number.NaN;

    // Step 1: filter by path match
    const pathMatched = traceUsageDetails.filter((detail) =>
      isPathMatch(logPath, normalizeTracePath(detail.__endpointPath))
    );
    if (pathMatched.length === 0) return [];

    // Step 2: try to extract model from log message, then filter by model
    const logModel = extractModelFromMessage(traceLogLine.message);
    const modelMatched = logModel
      ? pathMatched.filter((d) => d.__modelName?.toLowerCase() === logModel.toLowerCase())
      : [];

    // Step 3: prefer model-matched set; fall back to path-matched
    const useModelSet = modelMatched.length > 0;
    const source = useModelSet ? modelMatched : pathMatched;

    return source
      .map((detail) => {
        const timeDeltaMs =
          !Number.isNaN(logTimestampMs) && detail.__timestampMs > 0
            ? Math.abs(logTimestampMs - detail.__timestampMs)
            : null;
        return { detail, modelMatched: useModelSet, timeDeltaMs } satisfies TraceCandidate;
      })
      .sort((a, b) => (b.detail.__timestampMs || 0) - (a.detail.__timestampMs || 0))
      .slice(0, TRACE_MAX_CANDIDATES);
  }, [traceLogLine, traceUsageDetails]);

  const resolveTraceSourceInfo = useCallback(
    (sourceRaw: string, authIndex: unknown): SourceInfo =>
      resolveSourceDisplay(sourceRaw, authIndex, traceSourceInfoMap, traceAuthFileMap),
    [traceAuthFileMap, traceSourceInfoMap]
  );

  const openTraceModal = useCallback(
    (line: ParsedLogLine) => {
      if (!isTraceableRequestPath(line.path)) return;
      setTraceError('');
      traceLogLineRef.current = line;
      setTraceLogLine(line);
      void loadTraceUsageDetailsInternal(false, line);
    },
    [loadTraceUsageDetailsInternal]
  );

  const closeTraceModal = useCallback(() => {
    if (requestLogDownloading) return;
    traceLogLineRef.current = null;
    setTraceLogLine(null);
    setTraceUsageDetails([]);
  }, [requestLogDownloading]);

  return {
    traceLogLine,
    traceLoading,
    traceError,
    traceCandidates,
    resolveTraceSourceInfo,
    loadTraceUsageDetails,
    refreshTraceUsageDetails,
    openTraceModal,
    closeTraceModal,
  };
}
