import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Select } from '@/components/ui/Select';
import { authFilesApi } from '@/services/api/authFiles';
import type { UsageEventsPageData } from '@/services/api';
import type { GeminiKeyConfig, ProviderKeyConfig, OpenAIProviderConfig } from '@/types';
import type { AuthFileItem } from '@/types/authFile';
import type { CredentialInfo } from '@/types/sourceInfo';
import { buildSourceInfoMap } from '@/utils/sourceResolver';
import {
  calculateTokenCostForModel,
  normalizeAuthIndex,
  type ModelPrice
} from '@/utils/usage';
import { downloadBlob } from '@/utils/download';
import styles from '@/pages/UsagePage.module.scss';

const ALL_FILTER = '__all__';
const REQUEST_EVENTS_PAGE_SIZE_OPTIONS = [10, 20, 50, 100];

type RequestEventRow = {
  id: string;
  timestamp: string;
  timestampMs: number;
  timestampLabel: string;
  model: string;
  provider: string;
  authFile: string;
  failed: boolean;
  cost: number;
  costLabel: string;
  latencyMs: number;
  latencyLabel: string;
  firstTokenLatencyMs: number;
  firstTokenLabel: string;
  outputRate: number | null;
  outputRateLabel: string;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cachedTokens: number;
  totalTokens: number;
};

export interface RequestEventsDetailsCardProps {
  events: UsageEventsPageData | null;
  loading: boolean;
  page: number;
  pageSize: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  geminiKeys: GeminiKeyConfig[];
  claudeConfigs: ProviderKeyConfig[];
  codexConfigs: ProviderKeyConfig[];
  vertexConfigs: ProviderKeyConfig[];
  openaiProviders: OpenAIProviderConfig[];
  modelPrices: Record<string, ModelPrice>;
}

const encodeCsv = (value: string | number): string => {
  const text = String(value ?? '');
  const trimmedLeft = text.replace(/^\s+/, '');
  const safeText = trimmedLeft && /^[=+\-@]/.test(trimmedLeft) ? `'${text}` : text;
  return `"${safeText.replace(/"/g, '""')}"`;
};

const formatPreciseUsd = (value: number): string => {
  if (!Number.isFinite(value) || value <= 0) return '--';
  if (value < 0.000001) return '<$0.000001';
  if (value < 1) return `$${value.toFixed(6)}`;
  return `$${value.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  })}`;
};

const formatDurationMs = (value: number): string => {
  if (!Number.isFinite(value) || value <= 0) return '--';
  if (value < 1000) return `${Math.round(value)}ms`;

  const seconds = value / 1000;
  if (seconds < 10) return `${seconds.toFixed(1)}s`;
  if (seconds < 60) return `${Math.round(seconds)}s`;

  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = Math.round(seconds % 60);
  if (remainingSeconds <= 0) return `${minutes}m`;
  return `${minutes}m ${remainingSeconds}s`;
};

const calculateOutputRate = (outputTokens: number, latencyMs: number): number | null => {
  if (outputTokens <= 0 || latencyMs <= 0) return null;

  const totalSeconds = latencyMs / 1000;
  if (!Number.isFinite(totalSeconds) || totalSeconds <= 0) return null;

  const rate = outputTokens / totalSeconds;
  return Number.isFinite(rate) && rate > 0 ? rate : null;
};

const formatOutputRate = (value: number | null): string => {
  if (value === null || !Number.isFinite(value) || value <= 0) return '--';
  const formatted = value >= 100 ? value.toFixed(0) : value.toFixed(1);
  return `${formatted} tps`;
};

const normalizeProviderLabel = (value: unknown): string => {
  const text =
    typeof value === 'string'
      ? value.trim()
      : value === null || value === undefined
        ? ''
        : String(value).trim();
  return text ? text.toLowerCase() : '-';
};

const looksLikeAuthFileName = (value: string): boolean =>
  /\.[A-Za-z0-9_-]{2,16}$/i.test(value);

const resolveAuthFileName = (sourceRaw: string, authInfo?: CredentialInfo): string => {
  const authFileName = authInfo?.name?.trim();
  if (authFileName) return authFileName;

  const normalizedSource = sourceRaw.startsWith('t:') ? sourceRaw.slice(2) : sourceRaw;
  const sourceText = normalizedSource.trim();
  return sourceText && looksLikeAuthFileName(sourceText) ? sourceText : '-';
};

export function RequestEventsDetailsCard({
  events,
  loading,
  page,
  pageSize,
  onPageChange,
  onPageSizeChange,
  geminiKeys,
  claudeConfigs,
  codexConfigs,
  vertexConfigs,
  openaiProviders,
  modelPrices
}: RequestEventsDetailsCardProps) {
  const { t, i18n } = useTranslation();

  const [modelFilter, setModelFilter] = useState(ALL_FILTER);
  const [providerFilter, setProviderFilter] = useState(ALL_FILTER);
  const [authFileFilter, setAuthFileFilter] = useState(ALL_FILTER);
  const [authFileMap, setAuthFileMap] = useState<Map<string, CredentialInfo>>(new Map());

  useEffect(() => {
    let cancelled = false;
    authFilesApi
      .list()
      .then((res) => {
        if (cancelled) return;
        const files = Array.isArray(res) ? res : (res as { files?: AuthFileItem[] })?.files;
        if (!Array.isArray(files)) return;
        const map = new Map<string, CredentialInfo>();
        files.forEach((file) => {
          const key = normalizeAuthIndex(file['auth_index'] ?? file.authIndex);
          if (!key) return;
          map.set(key, {
            name: file.name || key,
            type: (file.type || file.provider || '').toString()
          });
        });
        setAuthFileMap(map);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  const sourceInfoMap = useMemo(
    () =>
      buildSourceInfoMap({
        geminiApiKeys: geminiKeys,
        claudeApiKeys: claudeConfigs,
        codexApiKeys: codexConfigs,
        vertexApiKeys: vertexConfigs,
        openaiCompatibility: openaiProviders,
      }),
    [claudeConfigs, codexConfigs, geminiKeys, openaiProviders, vertexConfigs]
  );

  const rows = useMemo<RequestEventRow[]>(() => {
    if (!events) return [];

    return events.items
      .map((item, index) => {
        const timestamp = item.timestamp;
        const timestampMs = Date.parse(timestamp);
        const date = Number.isNaN(timestampMs) ? null : new Date(timestampMs);
        const sourceRaw = String(item.source ?? '').trim();
        const authIndex = normalizeAuthIndex(item.authIndex) ?? '-';
        const sourceInfo = sourceInfoMap.get(sourceRaw);
        const authInfo = authIndex !== '-' ? authFileMap.get(authIndex) : undefined;
        const provider = normalizeProviderLabel(item.provider || sourceInfo?.type || authInfo?.type);
        const authFile = resolveAuthFileName(sourceRaw, authInfo);
        const model = item.model || '-';
        const inputTokens = Math.max(item.tokens.inputTokens, 0);
        const outputTokens = Math.max(item.tokens.outputTokens, 0);
        const reasoningTokens = Math.max(item.tokens.reasoningTokens, 0);
        const cachedTokens = Math.max(item.tokens.cachedTokens, 0);
        const totalTokens = Math.max(item.tokens.totalTokens, 0);
        const latencyMs = Math.max(item.latencyMs, 0);
        const firstTokenLatencyMs = Math.max(item.firstTokenLatencyMs, 0);
        const cost = calculateTokenCostForModel(
          model,
          { inputTokens, outputTokens, cachedTokens },
          modelPrices
        );
        const outputRate = calculateOutputRate(outputTokens, latencyMs);

        return {
          id: `${timestamp}-${item.model}-${provider}-${authFile}-${authIndex}-${index}`,
          timestamp,
          timestampMs: Number.isNaN(timestampMs) ? 0 : timestampMs,
          timestampLabel: date ? date.toLocaleString(i18n.language) : timestamp || '-',
          model,
          provider,
          authFile,
          failed: item.failed === true,
          cost,
          costLabel: formatPreciseUsd(cost),
          latencyMs,
          latencyLabel: formatDurationMs(latencyMs),
          firstTokenLatencyMs,
          firstTokenLabel: formatDurationMs(firstTokenLatencyMs),
          outputRate,
          outputRateLabel: formatOutputRate(outputRate),
          inputTokens,
          outputTokens,
          reasoningTokens,
          cachedTokens,
          totalTokens
        };
      })
      .sort((a, b) => b.timestampMs - a.timestampMs);
  }, [authFileMap, events, i18n.language, modelPrices, sourceInfoMap]);

  const modelOptions = useMemo(
    () => [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      ...Array.from(new Set(rows.map((row) => row.model))).map((model) => ({
        value: model,
        label: model
      }))
    ],
    [rows, t]
  );

  const providerOptions = useMemo(
    () => [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      ...Array.from(new Set(rows.map((row) => row.provider))).map((provider) => ({
        value: provider,
        label: provider
      }))
    ],
    [rows, t]
  );

  const authFileOptions = useMemo(
    () => [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      ...Array.from(new Set(rows.map((row) => row.authFile))).map((authFile) => ({
        value: authFile,
        label: authFile
      }))
    ],
    [rows, t]
  );

  const modelOptionSet = useMemo(() => new Set(modelOptions.map((option) => option.value)), [modelOptions]);
  const providerOptionSet = useMemo(
    () => new Set(providerOptions.map((option) => option.value)),
    [providerOptions]
  );
  const authFileOptionSet = useMemo(
    () => new Set(authFileOptions.map((option) => option.value)),
    [authFileOptions]
  );

  const effectiveModelFilter = modelOptionSet.has(modelFilter) ? modelFilter : ALL_FILTER;
  const effectiveProviderFilter = providerOptionSet.has(providerFilter) ? providerFilter : ALL_FILTER;
  const effectiveAuthFileFilter = authFileOptionSet.has(authFileFilter)
    ? authFileFilter
    : ALL_FILTER;

  const filteredRows = useMemo(
    () =>
      rows.filter((row) => {
        const modelMatched = effectiveModelFilter === ALL_FILTER || row.model === effectiveModelFilter;
        const providerMatched = effectiveProviderFilter === ALL_FILTER || row.provider === effectiveProviderFilter;
        const authFileMatched =
          effectiveAuthFileFilter === ALL_FILTER || row.authFile === effectiveAuthFileFilter;
        return modelMatched && providerMatched && authFileMatched;
      }),
    [effectiveAuthFileFilter, effectiveModelFilter, effectiveProviderFilter, rows]
  );

  const renderedRows = filteredRows;

  const safePage = Math.max(1, events?.page ?? page);
  const safePageSize = Math.max(1, events?.pageSize ?? pageSize);
  const hasNextPage = events?.hasMore === true;
  const pageSizeOptions = useMemo(
    () =>
      REQUEST_EVENTS_PAGE_SIZE_OPTIONS.map((size) => ({
        value: String(size),
        label: String(size)
      })),
    []
  );

  const handlePageSizeChange = (value: string) => {
    const nextPageSize = Number(value);
    onPageSizeChange(Number.isFinite(nextPageSize) ? nextPageSize : 10);
  };

  const hasActiveFilters =
    effectiveModelFilter !== ALL_FILTER ||
    effectiveProviderFilter !== ALL_FILTER ||
    effectiveAuthFileFilter !== ALL_FILTER;

  const handleClearFilters = () => {
    setModelFilter(ALL_FILTER);
    setProviderFilter(ALL_FILTER);
    setAuthFileFilter(ALL_FILTER);
  };

  const handleExportCsv = () => {
    if (!filteredRows.length) return;

    const csvHeader = [
      'timestamp',
      'model',
      'provider',
      'auth_file',
      'result',
      'cost_usd',
      'first_token_latency_ms',
      'latency_ms',
      'output_tokens_per_second',
      'input_tokens',
      'output_tokens',
      'reasoning_tokens',
      'cached_tokens',
      'total_tokens'
    ];

    const csvRows = filteredRows.map((row) =>
      [
        row.timestamp,
        row.model,
        row.provider,
        row.authFile,
        row.failed ? 'failed' : 'success',
        row.cost > 0 ? row.cost : '',
        row.firstTokenLatencyMs || '',
        row.latencyMs || '',
        row.outputRate ?? '',
        row.inputTokens,
        row.outputTokens,
        row.reasoningTokens,
        row.cachedTokens,
        row.totalTokens
      ]
        .map((value) => encodeCsv(value))
        .join(',')
    );

    const content = [csvHeader.join(','), ...csvRows].join('\n');
    const fileTime = new Date().toISOString().replace(/[:.]/g, '-');
    downloadBlob({
      filename: `usage-events-${fileTime}.csv`,
      blob: new Blob([content], { type: 'text/csv;charset=utf-8' })
    });
  };

  const handleExportJson = () => {
    if (!filteredRows.length) return;

    const payload = filteredRows.map((row) => ({
      timestamp: row.timestamp,
      model: row.model,
      provider: row.provider,
      auth_file: row.authFile,
      failed: row.failed,
      cost_usd: row.cost > 0 ? row.cost : null,
      first_token_latency_ms: row.firstTokenLatencyMs || null,
      latency_ms: row.latencyMs || null,
      output_tokens_per_second: row.outputRate,
      tokens: {
        input_tokens: row.inputTokens,
        output_tokens: row.outputTokens,
        reasoning_tokens: row.reasoningTokens,
        cached_tokens: row.cachedTokens,
        total_tokens: row.totalTokens
      }
    }));

    const content = JSON.stringify(payload, null, 2);
    const fileTime = new Date().toISOString().replace(/[:.]/g, '-');
    downloadBlob({
      filename: `usage-events-${fileTime}.json`,
      blob: new Blob([content], { type: 'application/json;charset=utf-8' })
    });
  };

  return (
    <Card
      title={t('usage_stats.request_events_title')}
      extra={
        <div className={styles.requestEventsActions}>
          <Button
            variant="ghost"
            size="sm"
            onClick={handleClearFilters}
            disabled={!hasActiveFilters}
          >
            {t('usage_stats.clear_filters')}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleExportCsv}
            disabled={filteredRows.length === 0}
          >
            {t('usage_stats.export_csv')}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleExportJson}
            disabled={filteredRows.length === 0}
          >
            {t('usage_stats.export_json')}
          </Button>
        </div>
      }
    >
      <div className={styles.requestEventsToolbar}>
        <div className={styles.requestEventsFilterItem}>
          <span className={styles.requestEventsFilterLabel}>
            {t('usage_stats.request_events_filter_model')}
          </span>
          <Select
            value={effectiveModelFilter}
            options={modelOptions}
            onChange={setModelFilter}
            className={styles.requestEventsSelect}
            ariaLabel={t('usage_stats.request_events_filter_model')}
            fullWidth={false}
          />
        </div>
        <div className={styles.requestEventsFilterItem}>
          <span className={styles.requestEventsFilterLabel}>
            {t('usage_stats.request_events_filter_source')}
          </span>
          <Select
            value={effectiveProviderFilter}
            options={providerOptions}
            onChange={setProviderFilter}
            className={styles.requestEventsSelect}
            ariaLabel={t('usage_stats.request_events_filter_source')}
            fullWidth={false}
          />
        </div>
        <div className={styles.requestEventsFilterItem}>
          <span className={styles.requestEventsFilterLabel}>
            {t('usage_stats.request_events_filter_auth_index')}
          </span>
          <Select
            value={effectiveAuthFileFilter}
            options={authFileOptions}
            onChange={setAuthFileFilter}
            className={styles.requestEventsSelect}
            ariaLabel={t('usage_stats.request_events_filter_auth_index')}
            fullWidth={false}
          />
        </div>
      </div>

      {loading && rows.length === 0 ? (
        <div className={styles.hint}>{t('common.loading')}</div>
      ) : rows.length === 0 ? (
        <EmptyState
          title={t('usage_stats.request_events_empty_title')}
          description={t('usage_stats.request_events_empty_desc')}
        />
      ) : filteredRows.length === 0 ? (
        <EmptyState
          title={t('usage_stats.request_events_no_result_title')}
          description={t('usage_stats.request_events_no_result_desc')}
        />
      ) : (
        <>
          <div className={styles.requestEventsMeta}>
            <span>{t('usage_stats.request_events_page_count', { count: filteredRows.length })}</span>
            <span className={styles.requestEventsLimitHint}>
              {events?.totalExact
                ? t('usage_stats.request_events_exact_total', { count: events.totalItems })
                : hasNextPage
                  ? t('usage_stats.request_events_more_hint')
                  : t('usage_stats.request_events_last_page_hint')}
            </span>
          </div>

          <div className={styles.requestEventsTableWrapper}>
            <table className={`${styles.table} ${styles.requestEventsTable}`}>
              <colgroup>
                <col className={styles.requestEventsColTimestamp} />
                <col className={styles.requestEventsColModel} />
                <col className={styles.requestEventsColProvider} />
                <col className={styles.requestEventsColAuthFile} />
                <col className={styles.requestEventsColResult} />
                <col className={styles.requestEventsColCost} />
                <col className={styles.requestEventsColPerf} />
                <col className={styles.requestEventsColInputTokens} />
                <col className={styles.requestEventsColOutputTokens} />
                <col className={styles.requestEventsColReasoningTokens} />
                <col className={styles.requestEventsColCachedTokens} />
                <col className={styles.requestEventsColTotalTokens} />
              </colgroup>
              <thead>
                <tr>
                  <th>{t('usage_stats.request_events_timestamp')}</th>
                  <th>{t('usage_stats.model_name')}</th>
                  <th>{t('usage_stats.request_events_source')}</th>
                  <th>{t('usage_stats.request_events_auth_index')}</th>
                  <th>{t('usage_stats.request_events_result')}</th>
                  <th>{t('usage_stats.request_events_cost')}</th>
                  <th>{t('usage_stats.request_events_perf')}</th>
                  <th>{t('usage_stats.input_tokens')}</th>
                  <th>{t('usage_stats.output_tokens')}</th>
                  <th>{t('usage_stats.reasoning_tokens')}</th>
                  <th>{t('usage_stats.cached_tokens')}</th>
                  <th>{t('usage_stats.total_tokens')}</th>
                </tr>
              </thead>
              <tbody>
                {renderedRows.map((row) => (
                  <tr key={row.id}>
                    <td title={row.timestamp} className={styles.requestEventsTimestamp}>
                      {row.timestampLabel}
                    </td>
                    <td className={styles.modelCell}>{row.model}</td>
                    <td className={styles.requestEventsProviderCell} title={row.provider}>
                      {row.provider}
                    </td>
                    <td className={styles.requestEventsAuthFile} title={row.authFile}>
                      {row.authFile}
                    </td>
                    <td>
                      <span
                        className={row.failed ? styles.requestEventsResultFailed : styles.requestEventsResultSuccess}
                      >
                        {row.failed ? t('stats.failure') : t('stats.success')}
                      </span>
                    </td>
                    <td className={styles.requestEventsCostCell}>{row.costLabel}</td>
                    <td>
                      <div className={styles.requestEventsPerfStack}>
                        <span className={styles.requestEventsPerfPillSuccess}>
                          {t('usage_stats.request_events_first_token')}: {row.firstTokenLabel}
                        </span>
                        <span className={styles.requestEventsPerfPill}>
                          {t('usage_stats.request_events_latency')}: {row.latencyLabel}
                        </span>
                        <span className={styles.requestEventsPerfPill}>
                          {t('usage_stats.request_events_output_rate')}: {row.outputRateLabel}
                        </span>
                      </div>
                    </td>
                    <td className={styles.requestEventsNumericCell}>{row.inputTokens.toLocaleString()}</td>
                    <td className={styles.requestEventsNumericCell}>{row.outputTokens.toLocaleString()}</td>
                    <td className={styles.requestEventsNumericCell}>{row.reasoningTokens.toLocaleString()}</td>
                    <td className={styles.requestEventsNumericCell}>{row.cachedTokens.toLocaleString()}</td>
                    <td className={styles.requestEventsNumericCell}>{row.totalTokens.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className={styles.requestEventsPagination}>
            <div className={`${styles.requestEventsFilterItem} ${styles.requestEventsPageSizeItem}`}>
              <span className={styles.requestEventsFilterLabel}>
                {t('usage_stats.request_events_page_size')}
              </span>
              <Select
                value={String(safePageSize)}
                options={pageSizeOptions}
                onChange={handlePageSizeChange}
                className={styles.requestEventsPageSizeSelect}
                ariaLabel={t('usage_stats.request_events_page_size')}
                fullWidth={false}
              />
            </div>
            <div className={styles.requestEventsPagerButtons}>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onPageChange(safePage - 1)}
                disabled={loading || safePage <= 1}
              >
                {t('pagination.prev')}
              </Button>
              <span className={styles.requestEventsCurrentPage}>
                {t('usage_stats.request_events_current_page', { page: safePage })}
              </span>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onPageChange(safePage + 1)}
                disabled={loading || !hasNextPage}
              >
                {t('pagination.next')}
              </Button>
            </div>
          </div>
        </>
      )}
    </Card>
  );
}
