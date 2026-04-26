import { useState, useMemo, useCallback, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler,
} from 'chart.js';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { LoadingSpinner } from '@/components/ui/LoadingSpinner';
import { Select } from '@/components/ui/Select';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useHeaderRefresh } from '@/hooks/useHeaderRefresh';
import { useInterval } from '@/hooks/useInterval';
import { getUsageTimeRangeHours, useThemeStore, useConfigStore } from '@/stores';
import {
  StatCards,
  UsageChart,
  ChartLineSelector,
  ApiDetailsCard,
  ModelStatsCard,
  PriceSettingsCard,
  CredentialStatsCard,
  RequestEventsDetailsCard,
  TokenBreakdownChart,
  CostTrendChart,
  ServiceHealthCard,
  useUsageData,
  useSparklines,
  useChartData,
} from '@/components/usage';
import {
  getModelNamesFromUsage,
  getApiStats,
  getModelStats,
  hasAnyResolvableModelPrice,
  filterUsageByTimeRange,
  type UsageTimeRange,
} from '@/utils/usage';
import styles from './UsagePage.module.scss';

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Title,
  Tooltip,
  Legend,
  Filler
);

const CHART_LINES_STORAGE_KEY = 'cli-proxy-usage-chart-lines-v1';
const TIME_RANGE_STORAGE_KEY = 'cli-proxy-usage-time-range-v1';
const DEFAULT_CHART_LINES = ['all'];
const DEFAULT_TIME_RANGE: UsageTimeRange = '24h';
const MAX_CHART_LINES = 9;
const AUTO_REFRESH_INTERVAL_MS = 10_000;
const TIME_RANGE_OPTIONS: ReadonlyArray<{ value: UsageTimeRange; labelKey: string }> = [
  { value: 'all', labelKey: 'usage_stats.range_all' },
  { value: '7h', labelKey: 'usage_stats.range_7h' },
  { value: '24h', labelKey: 'usage_stats.range_24h' },
  { value: '7d', labelKey: 'usage_stats.range_7d' },
];

const isUsageTimeRange = (value: unknown): value is UsageTimeRange =>
  value === '7h' || value === '24h' || value === '7d' || value === 'all';

const normalizeChartLines = (value: unknown, maxLines = MAX_CHART_LINES): string[] => {
  if (!Array.isArray(value)) {
    return DEFAULT_CHART_LINES;
  }

  const filtered = value
    .filter((item): item is string => typeof item === 'string')
    .map((item) => item.trim())
    .filter(Boolean)
    .slice(0, maxLines);

  return filtered.length ? filtered : DEFAULT_CHART_LINES;
};

const loadChartLines = (): string[] => {
  try {
    if (typeof localStorage === 'undefined') {
      return DEFAULT_CHART_LINES;
    }
    const raw = localStorage.getItem(CHART_LINES_STORAGE_KEY);
    if (!raw) {
      return DEFAULT_CHART_LINES;
    }
    return normalizeChartLines(JSON.parse(raw));
  } catch {
    return DEFAULT_CHART_LINES;
  }
};

const loadTimeRange = (): UsageTimeRange => {
  try {
    if (typeof localStorage === 'undefined') {
      return DEFAULT_TIME_RANGE;
    }
    const raw = localStorage.getItem(TIME_RANGE_STORAGE_KEY);
    return isUsageTimeRange(raw) ? raw : DEFAULT_TIME_RANGE;
  } catch {
    return DEFAULT_TIME_RANGE;
  }
};

export function UsagePage() {
  const { t } = useTranslation();
  const isMobile = useMediaQuery('(max-width: 768px)');
  const resolvedTheme = useThemeStore((state) => state.resolvedTheme);
  const isDark = resolvedTheme === 'dark';
  const config = useConfigStore((state) => state.config);

  const [chartLines, setChartLines] = useState<string[]>(loadChartLines);
  const [timeRange, setTimeRange] = useState<UsageTimeRange>(loadTimeRange);
  const [showLegacyDetails, setShowLegacyDetails] = useState(false);

  const {
    summary,
    health,
    events,
    loading,
    error,
    lastRefreshedAt,
    modelPrices,
    selectedPriceModel,
    setSelectedPriceModel,
    setModelPrices,
    loadUsage,
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
    savingSelectedPriceModel,
  } = useUsageData({ timeRange });

  useHeaderRefresh(loadUsage);

  const timeRangeOptions = useMemo(
    () =>
      TIME_RANGE_OPTIONS.map((opt) => ({
        value: opt.value,
        label: t(opt.labelKey),
      })),
    [t]
  );

  useEffect(() => {
    try {
      if (typeof localStorage === 'undefined') {
        return;
      }
      localStorage.setItem(CHART_LINES_STORAGE_KEY, JSON.stringify(chartLines));
    } catch {
      // Ignore storage errors.
    }
  }, [chartLines]);

  useEffect(() => {
    try {
      if (typeof localStorage === 'undefined') {
        return;
      }
      localStorage.setItem(TIME_RANGE_STORAGE_KEY, timeRange);
    } catch {
      // Ignore storage errors.
    }
  }, [timeRange]);

  useInterval(
    () => {
      if (loading || exporting || importing) {
        return;
      }
      void loadUsage().catch(() => {});
    },
    AUTO_REFRESH_INTERVAL_MS
  );

  const { costSparkline, requestsSparkline, tokensSparkline, rpmSparkline, tpmSparkline } =
    useSparklines({ timeRange, loading });

  const {
    requestsPeriod,
    setRequestsPeriod,
    tokensPeriod,
    setTokensPeriod,
    availableModels,
    requestsChartData,
    tokensChartData,
    requestsChartOptions,
    tokensChartOptions,
  } = useChartData({ timeRange, chartLines, isDark, isMobile });

  const handleChartLinesChange = useCallback((lines: string[]) => {
    setChartLines(normalizeChartLines(lines));
  }, []);

  const legacyFilteredUsage = useMemo(
    () => (legacyUsage ? filterUsageByTimeRange(legacyUsage, timeRange) : null),
    [legacyUsage, timeRange]
  );
  const compatibilityHourWindowHours = useMemo(
    () => getUsageTimeRangeHours(timeRange),
    [timeRange]
  );

  const modelNames = useMemo(
    () =>
      Array.from(
        new Set([
          ...availableModels,
          ...getModelNamesFromUsage(legacyFilteredUsage),
          ...Object.keys(modelPrices),
          selectedPriceModel,
        ])
      )
        .filter((name) => name.trim() !== '')
        .sort((left, right) => left.localeCompare(right)),
    [availableModels, legacyFilteredUsage, modelPrices, selectedPriceModel]
  );

  const apiStats = useMemo(
    () => getApiStats(legacyFilteredUsage, modelPrices),
    [legacyFilteredUsage, modelPrices]
  );
  const modelStats = useMemo(
    () => getModelStats(legacyFilteredUsage, modelPrices),
    [legacyFilteredUsage, modelPrices]
  );
  const legacyHasPrices = useMemo(
    () => hasAnyResolvableModelPrice(legacyFilteredUsage, modelPrices),
    [legacyFilteredUsage, modelPrices]
  );

  const handleLoadLegacyDetails = useCallback(async () => {
    setShowLegacyDetails(true);
    if (!legacyLoaded) {
      await loadLegacyUsage();
    }
  }, [legacyLoaded, loadLegacyUsage]);

  return (
    <div className={styles.container}>
      {loading && !summary && (
        <div className={styles.loadingOverlay} aria-busy="true">
          <div className={styles.loadingOverlayContent}>
            <LoadingSpinner size={28} className={styles.loadingOverlaySpinner} />
            <span className={styles.loadingOverlayText}>{t('common.loading')}</span>
          </div>
        </div>
      )}

      <div className={styles.header}>
        <h1 className={styles.pageTitle}>{t('usage_stats.title')}</h1>
        <div className={styles.headerActions}>
          <div className={styles.timeRangeGroup}>
            <span className={styles.timeRangeLabel}>{t('usage_stats.range_filter')}</span>
            <Select
              value={timeRange}
              options={timeRangeOptions}
              onChange={(value) => setTimeRange(value as UsageTimeRange)}
              className={styles.timeRangeSelectControl}
              ariaLabel={t('usage_stats.range_filter')}
              fullWidth={false}
            />
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleExport}
            loading={exporting}
            disabled={loading || importing}
          >
            {t('usage_stats.export')}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={handleImport}
            loading={importing}
            disabled={loading || exporting}
          >
            {t('usage_stats.import')}
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => void loadUsage().catch(() => {})}
            disabled={loading || exporting || importing}
          >
            {loading ? t('common.loading') : t('usage_stats.refresh')}
          </Button>
          <input
            ref={importInputRef}
            type="file"
            accept=".json,application/json"
            style={{ display: 'none' }}
            onChange={handleImportChange}
          />
          {lastRefreshedAt && (
            <span className={styles.lastRefreshed}>
              {t('usage_stats.last_updated')}: {lastRefreshedAt.toLocaleTimeString()}
            </span>
          )}
        </div>
      </div>

      {error && <div className={styles.errorBox}>{error}</div>}

      <StatCards
        summary={summary}
        loading={loading}
        sparklines={{
          cost: costSparkline,
          requests: requestsSparkline,
          tokens: tokensSparkline,
          rpm: rpmSparkline,
          tpm: tpmSparkline,
        }}
      />

      <ServiceHealthCard health={health} loading={loading} />

      <RequestEventsDetailsCard
        events={events}
        loading={loading}
        geminiKeys={config?.geminiApiKeys || []}
        claudeConfigs={config?.claudeApiKeys || []}
        codexConfigs={config?.codexApiKeys || []}
        vertexConfigs={config?.vertexApiKeys || []}
        openaiProviders={config?.openaiCompatibility || []}
        modelPrices={modelPrices}
      />

      <ChartLineSelector
        chartLines={chartLines}
        modelNames={modelNames}
        maxLines={MAX_CHART_LINES}
        onChange={handleChartLinesChange}
      />

      <div className={styles.chartsGrid}>
        <UsageChart
          title={t('usage_stats.requests_trend')}
          period={requestsPeriod}
          onPeriodChange={setRequestsPeriod}
          chartData={requestsChartData}
          chartOptions={requestsChartOptions}
          loading={loading}
          isMobile={isMobile}
          emptyText={t('usage_stats.no_data')}
        />
        <UsageChart
          title={t('usage_stats.tokens_trend')}
          period={tokensPeriod}
          onPeriodChange={setTokensPeriod}
          chartData={tokensChartData}
          chartOptions={tokensChartOptions}
          loading={loading}
          isMobile={isMobile}
          emptyText={t('usage_stats.no_data')}
        />
      </div>

      {showLegacyDetails ? (
        <>
          <Card
            title={t('usage_stats.legacy_details_title')}
            extra={
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void loadLegacyUsage().catch(() => {})}
                loading={legacyLoading}
              >
                {t('usage_stats.legacy_details_refresh')}
              </Button>
            }
          >
            <div className={styles.hint}>{t('usage_stats.legacy_details_note')}</div>
          </Card>

          {!legacyLoaded && legacyLoading ? (
            <Card title={t('usage_stats.legacy_details_title')}>
              <div className={styles.hint}>{t('common.loading')}</div>
            </Card>
          ) : (
            <>
              <TokenBreakdownChart
                usage={legacyFilteredUsage}
                loading={legacyLoading}
                isDark={isDark}
                isMobile={isMobile}
                hourWindowHours={compatibilityHourWindowHours}
              />

              <CostTrendChart
                usage={legacyFilteredUsage}
                loading={legacyLoading}
                isDark={isDark}
                isMobile={isMobile}
                modelPrices={modelPrices}
                hourWindowHours={compatibilityHourWindowHours}
              />

              <div className={styles.detailsGrid}>
                <ApiDetailsCard
                  apiStats={apiStats}
                  loading={legacyLoading}
                  hasPrices={legacyHasPrices}
                />
                <ModelStatsCard
                  modelStats={modelStats}
                  loading={legacyLoading}
                  hasPrices={legacyHasPrices}
                />
              </div>

              <CredentialStatsCard
                usage={legacyFilteredUsage}
                loading={legacyLoading}
                geminiKeys={config?.geminiApiKeys || []}
                claudeConfigs={config?.claudeApiKeys || []}
                codexConfigs={config?.codexApiKeys || []}
                vertexConfigs={config?.vertexApiKeys || []}
                openaiProviders={config?.openaiCompatibility || []}
              />
            </>
          )}
        </>
      ) : (
        <Card
          title={t('usage_stats.legacy_details_title')}
          extra={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void handleLoadLegacyDetails().catch(() => {})}
              loading={legacyLoading}
            >
              {t('usage_stats.legacy_details_load')}
            </Button>
          }
        >
          <div className={styles.hint}>{t('usage_stats.legacy_details_desc')}</div>
        </Card>
      )}

      <PriceSettingsCard
        modelNames={modelNames}
        selectedModel={selectedPriceModel}
        modelPrices={modelPrices}
        onSelectedModelChange={setSelectedPriceModel}
        onPricesChange={setModelPrices}
        saving={savingModelPrices || savingSelectedPriceModel}
      />
    </div>
  );
}
