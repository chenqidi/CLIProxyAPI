/**
 * 使用统计相关 API
 */

import { apiClient } from './client';
import {
  createEmptyStatusBarData,
  mergeStatusBarData,
  normalizeAuthIndex,
  normalizeUsageSourceId,
  type KeyStats,
  type ModelPrice,
  type StatusBarData,
  type StatusBlockDetail,
  type StatusBlockState,
} from '@/utils/usage';

const USAGE_TIMEOUT_MS = 60 * 1000;

export interface UsageExportPayload {
  version?: number;
  exported_at?: string;
  usage?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface UsageImportResponse {
  added?: number;
  skipped?: number;
  total_requests?: number;
  failed_requests?: number;
  [key: string]: unknown;
}

export interface UsageRangeQuery {
  start?: string;
  end?: string;
}

export type UsageStatusQuery = UsageRangeQuery;

export interface UsageChartQuery extends UsageRangeQuery {
  period?: 'hour' | 'day';
  metric?: 'requests' | 'tokens';
  hours?: number;
}

export interface UsageEventsQuery extends UsageRangeQuery {
  page?: number;
  pageSize?: number;
  model?: string;
  source?: string;
  authIndex?: string;
  requestMethod?: string;
  requestPath?: string;
  failed?: boolean;
}

interface TimeWindowResponse {
  window_start?: string;
  window_end?: string;
}

interface UsageStatusBarResponse {
  blocks?: unknown[];
  block_details?: unknown[];
  success_rate?: number;
  total_success?: number;
  total_failure?: number;
}

interface UsageStatusOverviewResponse extends TimeWindowResponse {
  service?: UsageStatusBarResponse;
  by_source?: Record<string, UsageStatusBarResponse>;
  by_auth_index?: Record<string, UsageStatusBarResponse>;
}

interface UsageSummaryResponse extends TimeWindowResponse {
  total_requests?: number;
  success_count?: number;
  failure_count?: number;
  total_tokens?: number;
  cached_tokens?: number;
  reasoning_tokens?: number;
  distinct_sources?: number;
  distinct_auth_indexes?: number;
  distinct_models?: number;
  requests_last_30m?: number;
  tokens_last_30m?: number;
  rpm_30m?: number;
  tpm_30m?: number;
  retention_days?: number;
}

interface UsageHealthResponse extends TimeWindowResponse {
  blocks?: unknown[];
  block_details?: unknown[];
  success_rate?: number;
  total_success?: number;
  total_failure?: number;
  rows?: number;
  cols?: number;
}

interface UsageChartResponse extends TimeWindowResponse {
  period?: string;
  metric?: string;
  labels?: unknown[];
  data_by_model?: Record<string, unknown>;
}

interface UsageEventTokensResponse {
  input_tokens?: number;
  output_tokens?: number;
  reasoning_tokens?: number;
  cached_tokens?: number;
  total_tokens?: number;
}

interface UsageEventItemResponse {
  timestamp?: string;
  provider?: string;
  model?: string;
  api_key?: string;
  request_method?: string;
  request_path?: string;
  auth_id?: string;
  auth_index?: string;
  source?: string;
  latency_ms?: number;
  failed?: boolean;
  tokens?: UsageEventTokensResponse;
}

interface UsageEventsPageResponse extends TimeWindowResponse {
  page?: number;
  page_size?: number;
  total_items?: number;
  has_more?: boolean;
  items?: UsageEventItemResponse[];
}

export interface UsageStatusOverview {
  windowStart: string | null;
  windowEnd: string | null;
  service: StatusBarData;
  bySource: Record<string, StatusBarData>;
  byAuthIndex: Record<string, StatusBarData>;
  keyStats: KeyStats;
}

export interface UsageSummaryData {
  windowStart: string | null;
  windowEnd: string | null;
  totalRequests: number;
  successCount: number;
  failureCount: number;
  totalTokens: number;
  cachedTokens: number;
  reasoningTokens: number;
  distinctSources: number;
  distinctAuthIndexes: number;
  distinctModels: number;
  requestsLast30m: number;
  tokensLast30m: number;
  rpm30m: number;
  tpm30m: number;
  retentionDays: number;
}

export interface UsageHealthData {
  windowStart: string | null;
  windowEnd: string | null;
  blocks: StatusBlockState[];
  blockDetails: StatusBlockDetail[];
  successRate: number;
  totalSuccess: number;
  totalFailure: number;
  rows: number;
  cols: number;
}

export interface UsageChartData {
  windowStart: string | null;
  windowEnd: string | null;
  period: 'hour' | 'day';
  metric: 'requests' | 'tokens';
  labels: string[];
  dataByModel: Record<string, number[]>;
}

export interface UsageEventTokens {
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cachedTokens: number;
  totalTokens: number;
}

export interface UsageEventItem {
  timestamp: string;
  provider: string;
  model: string;
  apiKey: string;
  requestMethod: string;
  requestPath: string;
  authId: string;
  authIndex: string;
  source: string;
  latencyMs: number;
  failed: boolean;
  tokens: UsageEventTokens;
}

export interface UsageEventsPageData {
  windowStart: string | null;
  windowEnd: string | null;
  page: number;
  pageSize: number;
  totalItems: number;
  hasMore: boolean;
  items: UsageEventItem[];
}

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value);

const toFiniteNumber = (value: unknown, fallback = 0) => {
  const numeric = Number(value);
  return Number.isFinite(numeric) ? numeric : fallback;
};

const toCount = (value: unknown, fallback = 0) => {
  const numeric = toFiniteNumber(value, fallback);
  if (numeric <= 0) return 0;
  return Math.round(numeric);
};

const toBoolean = (value: unknown, fallback = false) => {
  if (typeof value === 'boolean') return value;
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase();
    if (normalized === 'true') return true;
    if (normalized === 'false') return false;
  }
  return fallback;
};

const toText = (value: unknown): string => {
  if (typeof value === 'string') return value.trim();
  if (value === null || value === undefined) return '';
  return String(value).trim();
};

const adaptTimeWindow = (value: unknown) => {
  const record = isRecord(value) ? value : null;
  return {
    windowStart: typeof record?.window_start === 'string' ? record.window_start : null,
    windowEnd: typeof record?.window_end === 'string' ? record.window_end : null,
  };
};

const deriveStatusBarState = (success: number, failure: number): StatusBlockState => {
  const total = success + failure;
  if (total <= 0) return 'idle';
  if (failure <= 0) return 'success';
  if (success <= 0) return 'failure';
  return 'mixed';
};

const adaptStatusBlockDetail = (value: unknown, fallback: StatusBlockDetail): StatusBlockDetail => {
  const record = isRecord(value) ? value : null;
  const success = toCount(record?.success, fallback.success);
  const failure = toCount(record?.failure, fallback.failure);
  const total = success + failure;

  return {
    success,
    failure,
    rate: total > 0 ? success / total : -1,
    startTime: toFiniteNumber(record?.start_time_ms, fallback.startTime),
    endTime: toFiniteNumber(record?.end_time_ms, fallback.endTime),
  };
};

const adaptStatusBarData = (value: unknown): StatusBarData => {
  const empty = createEmptyStatusBarData();
  const record = isRecord(value) ? value : null;
  const rawBlockDetails = Array.isArray(record?.block_details) ? record.block_details : [];
  const lastEmptyDetail =
    empty.blockDetails[empty.blockDetails.length - 1] ?? empty.blockDetails[0];
  const blockDetails = rawBlockDetails.length
    ? rawBlockDetails.map((detail, idx) =>
        adaptStatusBlockDetail(detail, empty.blockDetails[idx] ?? lastEmptyDetail)
      )
    : empty.blockDetails;

  const derivedTotals = blockDetails.reduce(
    (acc, detail) => {
      acc.success += detail.success;
      acc.failure += detail.failure;
      return acc;
    },
    { success: 0, failure: 0 }
  );

  const totalSuccess =
    record && 'total_success' in record
      ? toCount(record.total_success, derivedTotals.success)
      : derivedTotals.success;
  const totalFailure =
    record && 'total_failure' in record
      ? toCount(record.total_failure, derivedTotals.failure)
      : derivedTotals.failure;
  const total = totalSuccess + totalFailure;

  return {
    blocks: blockDetails.map((detail) => deriveStatusBarState(detail.success, detail.failure)),
    blockDetails,
    successRate:
      total > 0
        ? record && 'success_rate' in record
          ? toFiniteNumber(record.success_rate, (totalSuccess / total) * 100)
          : (totalSuccess / total) * 100
        : 100,
    totalSuccess,
    totalFailure,
  };
};

const adaptGroupedStatusMap = (
  value: unknown,
  normalizeKey: (key: string) => string | null
): Record<string, StatusBarData> => {
  const record = isRecord(value) ? value : null;
  if (!record) return {};

  return Object.entries(record).reduce<Record<string, StatusBarData>>(
    (acc, [rawKey, rawStatus]) => {
      const key = normalizeKey(rawKey);
      if (!key) return acc;

      const adapted = adaptStatusBarData(rawStatus);
      acc[key] = acc[key] ? mergeStatusBarData([acc[key], adapted]) : adapted;
      return acc;
    },
    {}
  );
};

const buildKeyStatsFromStatusMaps = (
  bySource: Record<string, StatusBarData>,
  byAuthIndex: Record<string, StatusBarData>
): KeyStats => ({
  bySource: Object.fromEntries(
    Object.entries(bySource).map(([key, data]) => [
      key,
      { success: data.totalSuccess, failure: data.totalFailure },
    ])
  ),
  byAuthIndex: Object.fromEntries(
    Object.entries(byAuthIndex).map(([key, data]) => [
      key,
      { success: data.totalSuccess, failure: data.totalFailure },
    ])
  ),
});

const adaptUsageStatusOverview = (value: unknown): UsageStatusOverview => {
  const record = isRecord(value) ? value : null;
  const bySource = adaptGroupedStatusMap(record?.by_source, (key) => {
    const trimmed = key.trim();
    return trimmed ? normalizeUsageSourceId(trimmed) : null;
  });
  const byAuthIndex = adaptGroupedStatusMap(record?.by_auth_index, (key) =>
    normalizeAuthIndex(key)
  );

  return {
    ...adaptTimeWindow(record),
    service: adaptStatusBarData(record?.service),
    bySource,
    byAuthIndex,
    keyStats: buildKeyStatsFromStatusMaps(bySource, byAuthIndex),
  };
};

const adaptUsageSummary = (value: unknown): UsageSummaryData => {
  const record = isRecord(value) ? value : null;
  return {
    ...adaptTimeWindow(record),
    totalRequests: toCount(record?.total_requests),
    successCount: toCount(record?.success_count),
    failureCount: toCount(record?.failure_count),
    totalTokens: toCount(record?.total_tokens),
    cachedTokens: toCount(record?.cached_tokens),
    reasoningTokens: toCount(record?.reasoning_tokens),
    distinctSources: toCount(record?.distinct_sources),
    distinctAuthIndexes: toCount(record?.distinct_auth_indexes),
    distinctModels: toCount(record?.distinct_models),
    requestsLast30m: toCount(record?.requests_last_30m),
    tokensLast30m: toCount(record?.tokens_last_30m),
    rpm30m: toFiniteNumber(record?.rpm_30m),
    tpm30m: toFiniteNumber(record?.tpm_30m),
    retentionDays: toCount(record?.retention_days, 30),
  };
};

const adaptUsageHealth = (value: unknown): UsageHealthData => {
  const record = isRecord(value) ? value : null;
  const base = adaptStatusBarData(record);
  return {
    ...adaptTimeWindow(record),
    blocks: base.blocks,
    blockDetails: base.blockDetails,
    successRate: base.successRate,
    totalSuccess: base.totalSuccess,
    totalFailure: base.totalFailure,
    rows: toCount(record?.rows, 7),
    cols: toCount(record?.cols, 96),
  };
};

const adaptChartSeries = (value: unknown): number[] => {
  if (!Array.isArray(value)) return [];
  return value.map((item) => toFiniteNumber(item));
};

const adaptUsageChart = (value: unknown): UsageChartData => {
  const record = isRecord(value) ? value : null;
  const period = toText(record?.period) === 'hour' ? 'hour' : 'day';
  const metric = toText(record?.metric) === 'tokens' ? 'tokens' : 'requests';
  const labels = Array.isArray(record?.labels)
    ? record.labels.map((label) => toText(label)).filter(Boolean)
    : [];
  const rawDataByModel = isRecord(record?.data_by_model) ? record.data_by_model : null;
  const dataByModel = rawDataByModel
    ? Object.fromEntries(
        Object.entries(rawDataByModel).map(([model, series]) => [
          toText(model) || 'unknown',
          adaptChartSeries(series),
        ])
      )
    : {};

  return {
    ...adaptTimeWindow(record),
    period,
    metric,
    labels,
    dataByModel,
  };
};

const adaptUsageEventTokens = (value: unknown): UsageEventTokens => {
  const record = isRecord(value) ? value : null;
  return {
    inputTokens: toCount(record?.input_tokens),
    outputTokens: toCount(record?.output_tokens),
    reasoningTokens: toCount(record?.reasoning_tokens),
    cachedTokens: toCount(record?.cached_tokens),
    totalTokens: toCount(record?.total_tokens),
  };
};

const adaptUsageEventItem = (value: unknown): UsageEventItem => {
  const record = isRecord(value) ? value : null;
  const rawSource = toText(record?.source);
  return {
    timestamp: typeof record?.timestamp === 'string' ? record.timestamp : '',
    provider: toText(record?.provider) || 'unknown',
    model: toText(record?.model) || 'unknown',
    apiKey: toText(record?.api_key),
    requestMethod: toText(record?.request_method).toUpperCase(),
    requestPath: toText(record?.request_path),
    authId: toText(record?.auth_id),
    authIndex: normalizeAuthIndex(record?.auth_index) ?? '',
    source: rawSource ? normalizeUsageSourceId(rawSource) : '',
    latencyMs: toCount(record?.latency_ms),
    failed: toBoolean(record?.failed),
    tokens: adaptUsageEventTokens(record?.tokens),
  };
};

const adaptUsageEventsPage = (value: unknown): UsageEventsPageData => {
  const record = isRecord(value) ? value : null;
  const items = Array.isArray(record?.items) ? record.items.map(adaptUsageEventItem) : [];
  return {
    ...adaptTimeWindow(record),
    page: toCount(record?.page, 1),
    pageSize: toCount(record?.page_size, 100),
    totalItems: toCount(record?.total_items),
    hasMore: toBoolean(record?.has_more),
    items,
  };
};

const buildUsageEventsParams = (query: UsageEventsQuery) => ({
  start: query.start,
  end: query.end,
  page: query.page,
  page_size: query.pageSize,
  model: query.model,
  source: query.source,
  auth_index: query.authIndex,
  request_method: query.requestMethod,
  request_path: query.requestPath,
  failed: query.failed,
});

export const usageApi = {
  /**
   * 获取轻量 status 概览
   */
  async getUsageStatus(params: UsageStatusQuery = {}): Promise<UsageStatusOverview> {
    const response = await apiClient.get<UsageStatusOverviewResponse>('/usage/status', {
      params,
      timeout: USAGE_TIMEOUT_MS,
    });
    return adaptUsageStatusOverview(response);
  },

  /**
   * 获取 summary 概览
   */
  async getUsageSummary(params: UsageRangeQuery = {}): Promise<UsageSummaryData> {
    const response = await apiClient.get<UsageSummaryResponse>('/usage/summary', {
      params,
      timeout: USAGE_TIMEOUT_MS,
    });
    return adaptUsageSummary(response);
  },

  /**
   * 获取 7x96 健康网格
   */
  async getUsageHealth(params: UsageRangeQuery = {}): Promise<UsageHealthData> {
    const response = await apiClient.get<UsageHealthResponse>('/usage/health', {
      params,
      timeout: USAGE_TIMEOUT_MS,
    });
    return adaptUsageHealth(response);
  },

  /**
   * 获取 chart 系列
   */
  async getUsageChart(params: UsageChartQuery): Promise<UsageChartData> {
    const response = await apiClient.get<UsageChartResponse>('/usage/charts', {
      params,
      timeout: USAGE_TIMEOUT_MS,
    });
    return adaptUsageChart(response);
  },

  /**
   * 获取轻量事件列表
   */
  async getUsageEvents(params: UsageEventsQuery = {}): Promise<UsageEventsPageData> {
    const response = await apiClient.get<UsageEventsPageResponse>('/usage/events', {
      params: buildUsageEventsParams(params),
      timeout: USAGE_TIMEOUT_MS,
    });
    return adaptUsageEventsPage(response);
  },

  /**
   * 导出使用统计快照
   */
  exportUsage: () =>
    apiClient.get<UsageExportPayload>('/usage/export', { timeout: USAGE_TIMEOUT_MS }),

  /**
   * 导入使用统计快照
   */
  importUsage: (payload: unknown) =>
    apiClient.post<UsageImportResponse>('/usage/import', payload, { timeout: USAGE_TIMEOUT_MS }),

  /**
   * 更新服务端保存的模型价格覆盖
   */
  updateModelPrices: (prices: Record<string, ModelPrice>) =>
    apiClient.put('/usage-model-prices', prices, { timeout: USAGE_TIMEOUT_MS }),

  /**
   * 更新服务端保存的模型价格默认选择
   */
  updateSelectedModel: (model: string) =>
    apiClient.put('/usage-price-selected-model', { value: model }, { timeout: USAGE_TIMEOUT_MS }),
};
