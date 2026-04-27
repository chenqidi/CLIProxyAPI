import { create } from 'zustand';
import {
  usageApi,
  type UsageChartData,
  type UsageEventsPageData,
  type UsageHealthData,
  type UsageSummaryData,
} from '@/services/api';
import { useAuthStore } from '@/stores/useAuthStore';
import type { UsageTimeRange } from '@/utils/usage';

export const USAGE_DASHBOARD_STALE_TIME_MS = 240_000;

const HOUR_MS = 60 * 60 * 1000;
const DAY_MS = 24 * HOUR_MS;

type CacheEntry<T> = {
  data: T;
  loadedAt: number;
};

export type LoadDashboardDataOptions = {
  force?: boolean;
  staleTimeMs?: number;
};

export type UsageDashboardChartQuery = {
  range: UsageTimeRange;
  period: 'hour' | 'day';
  metric: 'requests' | 'tokens' | 'cost';
  hours?: number;
};

export type UsageDashboardEventsQuery = {
  range: UsageTimeRange;
  page?: number;
  pageSize?: number;
  model?: string;
  source?: string;
  authIndex?: string;
  failed?: boolean;
};

type UsageDashboardState = {
  summaryCache: Record<string, CacheEntry<UsageSummaryData>>;
  healthCache: Record<string, CacheEntry<UsageHealthData>>;
  chartCache: Record<string, CacheEntry<UsageChartData>>;
  eventsCache: Record<string, CacheEntry<UsageEventsPageData>>;
  scopeKey: string;
  loadUsageSummary: (range: UsageTimeRange, options?: LoadDashboardDataOptions) => Promise<UsageSummaryData>;
  loadUsageHealth: (options?: LoadDashboardDataOptions) => Promise<UsageHealthData>;
  loadUsageChart: (
    query: UsageDashboardChartQuery,
    options?: LoadDashboardDataOptions
  ) => Promise<UsageChartData>;
  loadUsageEvents: (
    query: UsageDashboardEventsQuery,
    options?: LoadDashboardDataOptions
  ) => Promise<UsageEventsPageData>;
  clearUsageDashboard: () => void;
};

type DashboardSetState = {
  (
    partial:
      | UsageDashboardState
      | Partial<UsageDashboardState>
      | ((state: UsageDashboardState) => UsageDashboardState | Partial<UsageDashboardState>),
    replace?: false
  ): void;
  (
    state: UsageDashboardState | ((state: UsageDashboardState) => UsageDashboardState),
    replace: true
  ): void;
};

let dashboardScopeToken = 0;
let dashboardRequestToken = 0;
const inFlightDashboardRequests = new Map<string, Promise<unknown>>();
const activeDashboardRequestTokens = new Map<string, number>();

const getScopeKey = () => {
  const { apiBase = '', managementKey = '' } = useAuthStore.getState();
  return `${apiBase}::${managementKey}`;
};

const normalizeRangeDurationMs = (range: UsageTimeRange) => {
  switch (range) {
    case '7h':
      return 7 * HOUR_MS;
    case '24h':
      return 24 * HOUR_MS;
    case '7d':
      return 7 * DAY_MS;
    case 'all':
    default:
      return 30 * DAY_MS;
  }
};

export const getUsageTimeRangeHours = (range: UsageTimeRange) =>
  Math.max(1, Math.round(normalizeRangeDurationMs(range) / HOUR_MS));

export const buildUsageRangeQuery = (range: UsageTimeRange, nowMs: number = Date.now()) => ({
  start: new Date(nowMs - normalizeRangeDurationMs(range)).toISOString(),
  end: new Date(nowMs).toISOString(),
});

export const buildUsageSummaryCacheKey = (range: UsageTimeRange) => `summary:${range}`;
export const buildUsageHealthCacheKey = () => 'health:default';
export const buildUsageChartCacheKey = (query: UsageDashboardChartQuery) =>
  `chart:${query.metric}:${query.period}:${query.range}:${query.hours ?? ''}`;
export const buildUsageEventsCacheKey = (query: UsageDashboardEventsQuery) =>
  `events:${query.range}:${query.page ?? 1}:${query.pageSize ?? 500}:${query.model ?? ''}:${query.source ?? ''}:${query.authIndex ?? ''}:${query.failed === undefined ? 'all' : query.failed ? '1' : '0'}`;

const isFresh = (loadedAt: number, staleTimeMs: number) => Date.now() - loadedAt < staleTimeMs;

const clearInFlightForScope = (scopeKey: string) => {
  Array.from(inFlightDashboardRequests.keys()).forEach((key) => {
    if (key.startsWith(`${scopeKey}::`)) {
      inFlightDashboardRequests.delete(key);
      activeDashboardRequestTokens.delete(key);
    }
  });
};

const nextDashboardRequestToken = (inFlightKey: string) => {
  dashboardRequestToken += 1;
  activeDashboardRequestTokens.set(inFlightKey, dashboardRequestToken);
  return dashboardRequestToken;
};

const isActiveDashboardRequest = (inFlightKey: string, requestToken: number) =>
  activeDashboardRequestTokens.get(inFlightKey) === requestToken;

const clearActiveDashboardRequest = (
  inFlightKey: string,
  requestToken: number,
  requestPromise: Promise<unknown>
) => {
  if (isActiveDashboardRequest(inFlightKey, requestToken)) {
    activeDashboardRequestTokens.delete(inFlightKey);
  }
  if (inFlightDashboardRequests.get(inFlightKey) === requestPromise) {
    inFlightDashboardRequests.delete(inFlightKey);
  }
};

const ensureDashboardScope = (
  set: DashboardSetState,
  get: () => UsageDashboardState
) => {
  const scopeKey = getScopeKey();
  if (get().scopeKey === scopeKey) {
    return scopeKey;
  }

  dashboardScopeToken += 1;
  clearInFlightForScope(get().scopeKey);
  set({
    summaryCache: {},
    healthCache: {},
    chartCache: {},
    eventsCache: {},
    scopeKey,
  });
  return scopeKey;
};

export const useUsageDashboardStore = create<UsageDashboardState>((set, get) => ({
  summaryCache: {},
  healthCache: {},
  chartCache: {},
  eventsCache: {},
  scopeKey: '',

  loadUsageSummary: async (range, options = {}) => {
    const scopeKey = ensureDashboardScope(set, get);
    const staleTimeMs = options.staleTimeMs ?? USAGE_DASHBOARD_STALE_TIME_MS;
    const cacheKey = buildUsageSummaryCacheKey(range);
    const cached = get().summaryCache[cacheKey];
    if (!options.force && cached && isFresh(cached.loadedAt, staleTimeMs)) {
      return cached.data;
    }

    const inFlightKey = `${scopeKey}::${cacheKey}`;
    const existing = inFlightDashboardRequests.get(inFlightKey) as Promise<UsageSummaryData> | undefined;
    if (existing && !options.force) return existing;

    const requestToken = dashboardScopeToken;
    const activeRequestToken = nextDashboardRequestToken(inFlightKey);
    const requestPromise = (async () => {
      const data = await usageApi.getUsageSummary(buildUsageRangeQuery(range));
      if (
        requestToken === dashboardScopeToken &&
        get().scopeKey === scopeKey &&
        isActiveDashboardRequest(inFlightKey, activeRequestToken)
      ) {
        set((state) => ({
          summaryCache: {
            ...state.summaryCache,
            [cacheKey]: { data, loadedAt: Date.now() },
          },
        }));
      }
      return data;
    })().finally(() => {
      clearActiveDashboardRequest(inFlightKey, activeRequestToken, requestPromise);
    });

    inFlightDashboardRequests.set(inFlightKey, requestPromise);
    return requestPromise;
  },

  loadUsageHealth: async (options = {}) => {
    const scopeKey = ensureDashboardScope(set, get);
    const staleTimeMs = options.staleTimeMs ?? USAGE_DASHBOARD_STALE_TIME_MS;
    const cacheKey = buildUsageHealthCacheKey();
    const cached = get().healthCache[cacheKey];
    if (!options.force && cached && isFresh(cached.loadedAt, staleTimeMs)) {
      return cached.data;
    }

    const inFlightKey = `${scopeKey}::${cacheKey}`;
    const existing = inFlightDashboardRequests.get(inFlightKey) as Promise<UsageHealthData> | undefined;
    if (existing && !options.force) return existing;

    const requestToken = dashboardScopeToken;
    const activeRequestToken = nextDashboardRequestToken(inFlightKey);
    const requestPromise = (async () => {
      const data = await usageApi.getUsageHealth();
      if (
        requestToken === dashboardScopeToken &&
        get().scopeKey === scopeKey &&
        isActiveDashboardRequest(inFlightKey, activeRequestToken)
      ) {
        set((state) => ({
          healthCache: {
            ...state.healthCache,
            [cacheKey]: { data, loadedAt: Date.now() },
          },
        }));
      }
      return data;
    })().finally(() => {
      clearActiveDashboardRequest(inFlightKey, activeRequestToken, requestPromise);
    });

    inFlightDashboardRequests.set(inFlightKey, requestPromise);
    return requestPromise;
  },

  loadUsageChart: async (query, options = {}) => {
    const scopeKey = ensureDashboardScope(set, get);
    const staleTimeMs = options.staleTimeMs ?? USAGE_DASHBOARD_STALE_TIME_MS;
    const cacheKey = buildUsageChartCacheKey(query);
    const cached = get().chartCache[cacheKey];
    if (!options.force && cached && isFresh(cached.loadedAt, staleTimeMs)) {
      return cached.data;
    }

    const inFlightKey = `${scopeKey}::${cacheKey}`;
    const existing = inFlightDashboardRequests.get(inFlightKey) as Promise<UsageChartData> | undefined;
    if (existing && !options.force) return existing;

    const requestToken = dashboardScopeToken;
    const activeRequestToken = nextDashboardRequestToken(inFlightKey);
    const requestPromise = (async () => {
      const hours = query.period === 'hour' ? query.hours ?? getUsageTimeRangeHours(query.range) : undefined;
      const data = await usageApi.getUsageChart({
        ...buildUsageRangeQuery(query.range),
        period: query.period,
        metric: query.metric,
        hours,
      });
      if (
        requestToken === dashboardScopeToken &&
        get().scopeKey === scopeKey &&
        isActiveDashboardRequest(inFlightKey, activeRequestToken)
      ) {
        set((state) => ({
          chartCache: {
            ...state.chartCache,
            [cacheKey]: { data, loadedAt: Date.now() },
          },
        }));
      }
      return data;
    })().finally(() => {
      clearActiveDashboardRequest(inFlightKey, activeRequestToken, requestPromise);
    });

    inFlightDashboardRequests.set(inFlightKey, requestPromise);
    return requestPromise;
  },

  loadUsageEvents: async (query, options = {}) => {
    const scopeKey = ensureDashboardScope(set, get);
    const staleTimeMs = options.staleTimeMs ?? USAGE_DASHBOARD_STALE_TIME_MS;
    const cacheKey = buildUsageEventsCacheKey(query);
    const cached = get().eventsCache[cacheKey];
    if (!options.force && cached && isFresh(cached.loadedAt, staleTimeMs)) {
      return cached.data;
    }

    const inFlightKey = `${scopeKey}::${cacheKey}`;
    const existing = inFlightDashboardRequests.get(inFlightKey) as Promise<UsageEventsPageData> | undefined;
    if (existing && !options.force) return existing;

    const requestToken = dashboardScopeToken;
    const activeRequestToken = nextDashboardRequestToken(inFlightKey);
    const requestPromise = (async () => {
      const data = await usageApi.getUsageEvents({
        ...buildUsageRangeQuery(query.range),
        page: query.page,
        pageSize: query.pageSize,
        model: query.model,
        source: query.source,
        authIndex: query.authIndex,
        failed: query.failed,
      });
      if (
        requestToken === dashboardScopeToken &&
        get().scopeKey === scopeKey &&
        isActiveDashboardRequest(inFlightKey, activeRequestToken)
      ) {
        set((state) => ({
          eventsCache: {
            ...state.eventsCache,
            [cacheKey]: { data, loadedAt: Date.now() },
          },
        }));
      }
      return data;
    })().finally(() => {
      clearActiveDashboardRequest(inFlightKey, activeRequestToken, requestPromise);
    });

    inFlightDashboardRequests.set(inFlightKey, requestPromise);
    return requestPromise;
  },

  clearUsageDashboard: () => {
    dashboardScopeToken += 1;
    clearInFlightForScope(get().scopeKey);
    set({
      summaryCache: {},
      healthCache: {},
      chartCache: {},
      eventsCache: {},
      scopeKey: '',
    });
  },
}));
