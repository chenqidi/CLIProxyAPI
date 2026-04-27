import { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  buildUsageEventsCacheKey,
  buildUsageHealthCacheKey,
  buildUsageSummaryCacheKey,
  getUsageTimeRangeHours,
  USAGE_DASHBOARD_STALE_TIME_MS,
  useAuthStore,
  useConfigStore,
  useNotificationStore,
  useUsageDashboardStore,
} from '@/stores';
import type { UsageEventsPageData, UsageHealthData, UsageSummaryData } from '@/services/api';
import { usageApi } from '@/services/api/usage';
import { downloadBlob } from '@/utils/download';
import {
  buildModelPriceOverrides,
  mergeModelPricesWithDefaults,
  normalizeUsagePriceSelectedModel,
  type ModelPrice,
  type UsageTimeRange,
} from '@/utils/usage';

const DEFAULT_EVENTS_PAGE_SIZE = 10;
const CHART_REFRESH_STALE_TIME_MS = 60_000;

export interface UsagePayload {
  total_requests?: number;
  success_count?: number;
  failure_count?: number;
  total_tokens?: number;
  apis?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface UseUsageDataOptions {
  timeRange: UsageTimeRange;
}

export interface LoadUsageOptions {
  silent?: boolean;
  forceCharts?: boolean;
}

export interface UseUsageDataReturn {
  summary: UsageSummaryData | null;
  health: UsageHealthData | null;
  events: UsageEventsPageData | null;
  loading: boolean;
  refreshing: boolean;
  backgroundRefreshing: boolean;
  error: string;
  lastRefreshedAt: Date | null;
  eventsPage: number;
  eventsPageSize: number;
  modelPrices: Record<string, ModelPrice>;
  selectedPriceModel: string;
  setSelectedPriceModel: (model: string) => Promise<boolean>;
  setModelPrices: (
    prices: Record<string, ModelPrice>,
    options?: { action?: 'save' | 'delete' }
  ) => Promise<boolean>;
  loadUsage: (options?: LoadUsageOptions) => Promise<void>;
  setEventsPage: (page: number) => void;
  setEventsPageSize: (pageSize: number) => void;
  legacyUsage: UsagePayload | null;
  legacyLoading: boolean;
  legacyLoaded: boolean;
  loadLegacyUsage: () => Promise<void>;
  handleExport: () => Promise<void>;
  handleImport: () => void;
  handleImportChange: (event: React.ChangeEvent<HTMLInputElement>) => Promise<void>;
  importInputRef: React.RefObject<HTMLInputElement | null>;
  exporting: boolean;
  importing: boolean;
  savingModelPrices: boolean;
  savingSelectedPriceModel: boolean;
}

export function useUsageData(options: UseUsageDataOptions): UseUsageDataReturn {
  const { timeRange } = options;
  const { t } = useTranslation();
  const { showNotification } = useNotificationStore();
  const [eventsPage, setEventsPageState] = useState(1);
  const [eventsPageSize, setEventsPageSizeState] = useState(DEFAULT_EVENTS_PAGE_SIZE);
  const summaryKey = useMemo(() => buildUsageSummaryCacheKey(timeRange), [timeRange]);
  const healthKey = useMemo(() => buildUsageHealthCacheKey(), []);
  const eventsKey = useMemo(
    () =>
      buildUsageEventsCacheKey({
        range: timeRange,
        page: eventsPage,
        pageSize: eventsPageSize,
        includeTotal: false,
      }),
    [eventsPage, eventsPageSize, timeRange]
  );

  const summary = useUsageDashboardStore((state) => state.summaryCache[summaryKey]?.data ?? null);
  const health = useUsageDashboardStore((state) => state.healthCache[healthKey]?.data ?? null);
  const events = useUsageDashboardStore((state) => state.eventsCache[eventsKey]?.data ?? null);
  const loadUsageSummary = useUsageDashboardStore((state) => state.loadUsageSummary);
  const loadUsageHealth = useUsageDashboardStore((state) => state.loadUsageHealth);
  const loadUsageChart = useUsageDashboardStore((state) => state.loadUsageChart);
  const loadUsageEvents = useUsageDashboardStore((state) => state.loadUsageEvents);
  const authScopeKey = useAuthStore((state) => `${state.apiBase}::${state.managementKey}`);

  const configModelPrices = useConfigStore((state) => state.config?.usageModelPrices);
  const configSelectedPriceModel = useConfigStore((state) => state.config?.usagePriceSelectedModel);
  const updateConfigValue = useConfigStore((state) => state.updateConfigValue);

  const [isLoadingState, setLoading] = useState(() => summary === null || health === null || events === null);
  const [refreshing, setRefreshing] = useState(false);
  const [backgroundRefreshing, setBackgroundRefreshing] = useState(false);
  const [error, setError] = useState('');
  const [lastRefreshedAt, setLastRefreshedAt] = useState<Date | null>(null);
  const [exporting, setExporting] = useState(false);
  const [importing, setImporting] = useState(false);
  const [savingModelPrices, setSavingModelPrices] = useState(false);
  const [savingSelectedPriceModel, setSavingSelectedPriceModel] = useState(false);
  const [legacyUsage, setLegacyUsage] = useState<UsagePayload | null>(null);
  const [legacyLoading, setLegacyLoading] = useState(false);
  const [legacyLoaded, setLegacyLoaded] = useState(false);
  const importInputRef = useRef<HTMLInputElement | null>(null);
  const legacyRequestTokenRef = useRef(0);
  const hasDashboardDataRef = useRef(summary !== null && health !== null && events !== null);

  const modelPrices = useMemo(
    () => mergeModelPricesWithDefaults(configModelPrices ?? {}),
    [configModelPrices]
  );
  const selectedPriceModel = useMemo(
    () => normalizeUsagePriceSelectedModel(configSelectedPriceModel),
    [configSelectedPriceModel]
  );

  useEffect(() => {
    setEventsPageState(1);
  }, [timeRange]);

  useEffect(() => {
    hasDashboardDataRef.current = summary !== null && health !== null && events !== null;
  }, [events, health, summary]);

  const loading = isLoadingState || summary === null || health === null || events === null;

  const loadDashboardCharts = useCallback(async (force: boolean) => {
    await Promise.all([
      loadUsageChart(
        {
          range: timeRange,
          period: 'hour',
          metric: 'requests',
          hours: getUsageTimeRangeHours(timeRange),
        },
        { force, staleTimeMs: CHART_REFRESH_STALE_TIME_MS }
      ),
      loadUsageChart(
        {
          range: timeRange,
          period: 'hour',
          metric: 'tokens',
          hours: getUsageTimeRangeHours(timeRange),
        },
        { force, staleTimeMs: CHART_REFRESH_STALE_TIME_MS }
      ),
      loadUsageChart(
        {
          range: timeRange,
          period: 'hour',
          metric: 'cost',
          hours: getUsageTimeRangeHours(timeRange),
        },
        { force, staleTimeMs: CHART_REFRESH_STALE_TIME_MS }
      ),
      loadUsageChart(
        {
          range: timeRange,
          period: 'day',
          metric: 'requests',
        },
        { force, staleTimeMs: CHART_REFRESH_STALE_TIME_MS }
      ),
      loadUsageChart(
        {
          range: timeRange,
          period: 'day',
          metric: 'tokens',
        },
        { force, staleTimeMs: CHART_REFRESH_STALE_TIME_MS }
      ),
    ]);
  }, [loadUsageChart, timeRange]);

  const loadDashboardData = useCallback(async (
    force: boolean,
    options: LoadUsageOptions = {}
  ) => {
    const silent = options.silent === true;

    if (hasDashboardDataRef.current) {
      if (silent) {
        setBackgroundRefreshing(true);
      } else {
        setRefreshing(true);
      }
    } else {
      setLoading(true);
    }
    setError('');
    try {
      await loadUsageEvents(
        { range: timeRange, page: eventsPage, pageSize: eventsPageSize, includeTotal: false },
        { force, staleTimeMs: USAGE_DASHBOARD_STALE_TIME_MS }
      );

      await Promise.all([
        loadUsageSummary(timeRange, { force, staleTimeMs: USAGE_DASHBOARD_STALE_TIME_MS }),
        loadUsageHealth({ force, staleTimeMs: USAGE_DASHBOARD_STALE_TIME_MS }),
      ]);
      void loadDashboardCharts(options.forceCharts === true || (force && !silent)).catch(() => {});
      setLastRefreshedAt(new Date());
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : '';
      setError(message || t('usage_stats.loading_error'));
      throw err;
    } finally {
      setLoading(false);
      setRefreshing(false);
      setBackgroundRefreshing(false);
    }
  }, [eventsPage, eventsPageSize, loadDashboardCharts, loadUsageEvents, loadUsageHealth, loadUsageSummary, t, timeRange]);

  const loadUsage = useCallback(async (options: LoadUsageOptions = {}) => {
    await loadDashboardData(true, options);
  }, [loadDashboardData]);

  const setEventsPage = useCallback((page: number) => {
    setEventsPageState(Number.isFinite(page) ? Math.max(1, Math.round(page)) : 1);
  }, []);

  const setEventsPageSize = useCallback((pageSize: number) => {
    const safePageSize = Number.isFinite(pageSize) ? Math.max(1, Math.round(pageSize)) : DEFAULT_EVENTS_PAGE_SIZE;
    setEventsPageSizeState(safePageSize);
    setEventsPageState(1);
  }, []);

  useEffect(() => {
    void loadDashboardData(false, { silent: true }).catch(() => {});
  }, [loadDashboardData]);

  useEffect(() => {
    legacyRequestTokenRef.current += 1;
    setLegacyUsage(null);
    setLegacyLoaded(false);
    setLegacyLoading(false);
  }, [authScopeKey]);

  const loadLegacyUsage = useCallback(async () => {
    const requestToken = legacyRequestTokenRef.current + 1;
    legacyRequestTokenRef.current = requestToken;
    setLegacyLoading(true);
    try {
      const data = await usageApi.exportUsage();
      if (legacyRequestTokenRef.current !== requestToken) {
        return;
      }
      const rawUsage = data?.usage;
      const nextUsage =
        rawUsage && typeof rawUsage === 'object' ? (rawUsage as UsagePayload) : null;
      setLegacyUsage(nextUsage);
      setLegacyLoaded(true);
    } finally {
      if (legacyRequestTokenRef.current === requestToken) {
        setLegacyLoading(false);
      }
    }
  }, []);

  const handleExport = async () => {
    setExporting(true);
    try {
      const data = await usageApi.exportUsage();
      const exportedAt =
        typeof data?.exported_at === 'string' ? new Date(data.exported_at) : new Date();
      const safeTimestamp = Number.isNaN(exportedAt.getTime())
        ? new Date().toISOString()
        : exportedAt.toISOString();
      const filename = `usage-export-${safeTimestamp.replace(/[:.]/g, '-')}.json`;
      downloadBlob({
        filename,
        blob: new Blob([JSON.stringify(data ?? {}, null, 2)], { type: 'application/json' })
      });
      showNotification(t('usage_stats.export_success'), 'success');
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : '';
      showNotification(
        `${t('notification.download_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
    } finally {
      setExporting(false);
    }
  };

  const handleImport = () => {
    importInputRef.current?.click();
  };

  const handleImportChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;

    setImporting(true);
    try {
      const text = await file.text();
      let payload: unknown;
      try {
        payload = JSON.parse(text);
      } catch {
        showNotification(t('usage_stats.import_invalid'), 'error');
        return;
      }

      const result = await usageApi.importUsage(payload);
      showNotification(
        t('usage_stats.import_success', {
          added: result?.added ?? 0,
          skipped: result?.skipped ?? 0,
          total: result?.total_requests ?? 0,
          failed: result?.failed_requests ?? 0
        }),
        'success'
      );

      try {
        await loadDashboardData(true, { silent: true, forceCharts: true });
        if (legacyLoaded) {
          await loadLegacyUsage();
        }
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : '';
        showNotification(
          `${t('notification.refresh_failed')}${message ? `: ${message}` : ''}`,
          'error'
        );
      }
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : '';
      showNotification(
        `${t('notification.upload_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
    } finally {
      setImporting(false);
    }
  };

  const handleSetSelectedPriceModel = useCallback(async (model: string) => {
    const normalized = normalizeUsagePriceSelectedModel(model);
    if (normalized === selectedPriceModel) {
      return true;
    }

    const previousModel = configSelectedPriceModel;
    updateConfigValue('usage-price-selected-model', normalized);
    setSavingSelectedPriceModel(true);
    try {
      await usageApi.updateSelectedModel(normalized);
      return true;
    } catch (err: unknown) {
      updateConfigValue('usage-price-selected-model', previousModel);
      const message = err instanceof Error ? err.message : '';
      showNotification(
        `${t('notification.update_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
      return false;
    } finally {
      setSavingSelectedPriceModel(false);
    }
  }, [configSelectedPriceModel, selectedPriceModel, showNotification, t, updateConfigValue]);

  const handleSetModelPrices = useCallback(async (
    prices: Record<string, ModelPrice>,
    options?: { action?: 'save' | 'delete' }
  ) => {
    const overrides = buildModelPriceOverrides(prices);
    setSavingModelPrices(true);
    try {
      await usageApi.updateModelPrices(overrides);
      updateConfigValue('usage-model-prices', overrides);
      try {
        await loadDashboardData(true, { silent: true, forceCharts: true });
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : '';
        showNotification(
          `${t('notification.refresh_failed')}${message ? `: ${message}` : ''}`,
          'error'
        );
      }
      showNotification(
        t(options?.action === 'delete' ? 'usage_stats.model_price_deleted' : 'usage_stats.model_price_saved'),
        'success'
      );
      return true;
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : '';
      showNotification(
        `${t('notification.update_failed')}${message ? `: ${message}` : ''}`,
        'error'
      );
      return false;
    } finally {
      setSavingModelPrices(false);
    }
  }, [loadDashboardData, showNotification, t, updateConfigValue]);

  return {
    summary,
    health,
    events,
    loading,
    refreshing,
    backgroundRefreshing,
    error,
    lastRefreshedAt,
    eventsPage,
    eventsPageSize,
    modelPrices,
    selectedPriceModel,
    setSelectedPriceModel: handleSetSelectedPriceModel,
    setModelPrices: handleSetModelPrices,
    loadUsage,
    setEventsPage,
    setEventsPageSize,
    legacyUsage,
    legacyLoading,
    legacyLoaded,
    loadLegacyUsage,
    handleExport,
    handleImport,
    handleImportChange,
    importInputRef,
    exporting,
    importing,
    savingModelPrices,
    savingSelectedPriceModel
  };
}
