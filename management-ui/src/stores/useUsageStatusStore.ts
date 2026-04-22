import { create } from 'zustand';
import { usageApi, type UsageStatusOverview } from '@/services/api';
import { useAuthStore } from '@/stores/useAuthStore';
import { createEmptyStatusBarData, type KeyStats, type StatusBarData } from '@/utils/usage';
import i18n from '@/i18n';

export const USAGE_STATUS_STALE_TIME_MS = 240_000;

export type LoadUsageStatusOptions = {
  force?: boolean;
  staleTimeMs?: number;
};

type UsageStatusState = {
  statusOverview: UsageStatusOverview | null;
  serviceStatus: StatusBarData;
  statusBySource: Record<string, StatusBarData>;
  statusByAuthIndex: Record<string, StatusBarData>;
  keyStats: KeyStats;
  loading: boolean;
  error: string | null;
  lastRefreshedAt: number | null;
  scopeKey: string;
  loadUsageStatus: (options?: LoadUsageStatusOptions) => Promise<void>;
  clearUsageStatus: () => void;
};

const createEmptyKeyStats = (): KeyStats => ({ bySource: {}, byAuthIndex: {} });

const createEmptyOverview = (): UsageStatusOverview => ({
  windowStart: null,
  windowEnd: null,
  service: createEmptyStatusBarData(),
  bySource: {},
  byAuthIndex: {},
  keyStats: createEmptyKeyStats(),
});

let usageStatusRequestToken = 0;
let inFlightUsageStatusRequest: { id: number; scopeKey: string; promise: Promise<void> } | null = null;

const getErrorMessage = (error: unknown) =>
  error instanceof Error
    ? error.message
    : typeof error === 'string'
      ? error
      : i18n.t('usage_stats.loading_error');

export const useUsageStatusStore = create<UsageStatusState>((set, get) => ({
  statusOverview: null,
  serviceStatus: createEmptyStatusBarData(),
  statusBySource: {},
  statusByAuthIndex: {},
  keyStats: createEmptyKeyStats(),
  loading: false,
  error: null,
  lastRefreshedAt: null,
  scopeKey: '',

  loadUsageStatus: async (options = {}) => {
    const force = options.force === true;
    const staleTimeMs = options.staleTimeMs ?? USAGE_STATUS_STALE_TIME_MS;
    const { apiBase = '', managementKey = '' } = useAuthStore.getState();
    const scopeKey = `${apiBase}::${managementKey}`;
    const state = get();
    const scopeChanged = state.scopeKey !== scopeKey;

    if (inFlightUsageStatusRequest && inFlightUsageStatusRequest.scopeKey === scopeKey) {
      await inFlightUsageStatusRequest.promise;
      return;
    }

    if (inFlightUsageStatusRequest && inFlightUsageStatusRequest.scopeKey !== scopeKey) {
      usageStatusRequestToken += 1;
      inFlightUsageStatusRequest = null;
    }

    const fresh =
      !scopeChanged &&
      state.lastRefreshedAt !== null &&
      Date.now() - state.lastRefreshedAt < staleTimeMs;

    if (!force && fresh) {
      return;
    }

    if (scopeChanged) {
      const emptyOverview = createEmptyOverview();
      set({
        statusOverview: null,
        serviceStatus: emptyOverview.service,
        statusBySource: emptyOverview.bySource,
        statusByAuthIndex: emptyOverview.byAuthIndex,
        keyStats: emptyOverview.keyStats,
        error: null,
        lastRefreshedAt: null,
        scopeKey,
      });
    }

    const requestId = (usageStatusRequestToken += 1);
    set({ loading: true, error: null, scopeKey });

    const requestPromise = (async () => {
      try {
        const overview = await usageApi.getUsageStatus();
        if (requestId !== usageStatusRequestToken) return;

        set({
          statusOverview: overview,
          serviceStatus: overview.service,
          statusBySource: overview.bySource,
          statusByAuthIndex: overview.byAuthIndex,
          keyStats: overview.keyStats,
          loading: false,
          error: null,
          lastRefreshedAt: Date.now(),
          scopeKey,
        });
      } catch (error: unknown) {
        if (requestId !== usageStatusRequestToken) return;
        const message = getErrorMessage(error);
        set({
          loading: false,
          error: message,
          scopeKey,
        });
        throw new Error(message);
      } finally {
        if (inFlightUsageStatusRequest?.id === requestId) {
          inFlightUsageStatusRequest = null;
        }
      }
    })();

    inFlightUsageStatusRequest = { id: requestId, scopeKey, promise: requestPromise };
    await requestPromise;
  },

  clearUsageStatus: () => {
    usageStatusRequestToken += 1;
    inFlightUsageStatusRequest = null;
    const emptyOverview = createEmptyOverview();
    set({
      statusOverview: null,
      serviceStatus: emptyOverview.service,
      statusBySource: emptyOverview.bySource,
      statusByAuthIndex: emptyOverview.byAuthIndex,
      keyStats: emptyOverview.keyStats,
      loading: false,
      error: null,
      lastRefreshedAt: null,
      scopeKey: '',
    });
  }
}));
