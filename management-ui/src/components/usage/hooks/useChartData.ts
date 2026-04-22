import { useEffect, useMemo, useState } from 'react';
import type { ChartOptions } from 'chart.js';
import {
  buildUsageChartCacheKey,
  getUsageTimeRangeHours,
  useUsageDashboardStore,
} from '@/stores';
import type { UsageChartData as UsageChartSeriesData } from '@/services/api';
import { buildChartOptions } from '@/utils/usage/chartConfig';
import type { ChartData, UsageTimeRange } from '@/utils/usage';

const CHART_COLORS = [
  { borderColor: '#8b8680', backgroundColor: 'rgba(139, 134, 128, 0.18)' },
  { borderColor: '#8b5cf6', backgroundColor: 'rgba(139, 92, 246, 0.18)' },
  { borderColor: '#22c55e', backgroundColor: 'rgba(34, 197, 94, 0.18)' },
  { borderColor: '#f97316', backgroundColor: 'rgba(249, 115, 22, 0.18)' },
  { borderColor: '#f59e0b', backgroundColor: 'rgba(245, 158, 11, 0.18)' },
  { borderColor: '#3b82f6', backgroundColor: 'rgba(59, 130, 246, 0.18)' },
] as const;

export interface UseChartDataOptions {
  timeRange: UsageTimeRange;
  chartLines: string[];
  isDark: boolean;
  isMobile: boolean;
}

export interface UseChartDataReturn {
  requestsPeriod: 'hour' | 'day';
  setRequestsPeriod: (period: 'hour' | 'day') => void;
  tokensPeriod: 'hour' | 'day';
  setTokensPeriod: (period: 'hour' | 'day') => void;
  availableModels: string[];
  requestsChartData: ChartData;
  tokensChartData: ChartData;
  requestsChartOptions: ChartOptions<'line'>;
  tokensChartOptions: ChartOptions<'line'>;
}

const buildChartDataFromSeries = (
  series: UsageChartSeriesData | null,
  selectedModels: string[]
): ChartData => {
  if (!series) {
    return { labels: [], datasets: [] };
  }

  const labels = series.labels;
  const modelsToShow = selectedModels.length > 0 ? selectedModels : ['all'];
  const getAllSeries = () =>
    labels.map((_, idx) =>
      Object.values(series.dataByModel).reduce((sum, values) => sum + (values[idx] ?? 0), 0)
    );

  return {
    labels,
    datasets: modelsToShow.map((model, index) => {
      const isAll = model === 'all';
      const data = isAll
        ? getAllSeries()
        : series.dataByModel[model] ?? new Array(labels.length).fill(0);
      const color = CHART_COLORS[index % CHART_COLORS.length];
      const shouldFill = modelsToShow.length === 1 || (isAll && modelsToShow.length > 1);
      return {
        label: isAll ? 'All Models' : model,
        data,
        borderColor: color.borderColor,
        backgroundColor: color.backgroundColor,
        pointBackgroundColor: color.borderColor,
        pointBorderColor: color.borderColor,
        fill: shouldFill,
        tension: 0.35,
      };
    }),
  };
};

export function useChartData({
  timeRange,
  chartLines,
  isDark,
  isMobile,
}: UseChartDataOptions): UseChartDataReturn {
  const [requestsPeriod, setRequestsPeriod] = useState<'hour' | 'day'>('day');
  const [tokensPeriod, setTokensPeriod] = useState<'hour' | 'day'>('day');
  const loadUsageChart = useUsageDashboardStore((state) => state.loadUsageChart);

  const requestsHours = requestsPeriod === 'hour' ? getUsageTimeRangeHours(timeRange) : undefined;
  const tokensHours = tokensPeriod === 'hour' ? getUsageTimeRangeHours(timeRange) : undefined;

  const requestsKey = useMemo(
    () =>
      buildUsageChartCacheKey({
        range: timeRange,
        period: requestsPeriod,
        metric: 'requests',
        hours: requestsHours,
      }),
    [requestsHours, requestsPeriod, timeRange]
  );
  const tokensKey = useMemo(
    () =>
      buildUsageChartCacheKey({
        range: timeRange,
        period: tokensPeriod,
        metric: 'tokens',
        hours: tokensHours,
      }),
    [timeRange, tokensHours, tokensPeriod]
  );

  const requestsSeries = useUsageDashboardStore((state) => state.chartCache[requestsKey]?.data ?? null);
  const tokensSeries = useUsageDashboardStore((state) => state.chartCache[tokensKey]?.data ?? null);

  useEffect(() => {
    void loadUsageChart(
      {
        range: timeRange,
        period: requestsPeriod,
        metric: 'requests',
        hours: requestsHours,
      },
      { force: false }
    ).catch(() => {});
  }, [loadUsageChart, requestsHours, requestsPeriod, timeRange]);

  useEffect(() => {
    void loadUsageChart(
      {
        range: timeRange,
        period: tokensPeriod,
        metric: 'tokens',
        hours: tokensHours,
      },
      { force: false }
    ).catch(() => {});
  }, [loadUsageChart, timeRange, tokensHours, tokensPeriod]);

  const requestsChartData = useMemo(
    () => buildChartDataFromSeries(requestsSeries, chartLines),
    [chartLines, requestsSeries]
  );

  const tokensChartData = useMemo(
    () => buildChartDataFromSeries(tokensSeries, chartLines),
    [chartLines, tokensSeries]
  );

  const requestsChartOptions = useMemo(
    () =>
      buildChartOptions({
        period: requestsPeriod,
        labels: requestsChartData.labels,
        isDark,
        isMobile
      }),
    [requestsPeriod, requestsChartData.labels, isDark, isMobile]
  );

  const tokensChartOptions = useMemo(
    () =>
      buildChartOptions({
        period: tokensPeriod,
        labels: tokensChartData.labels,
        isDark,
        isMobile
      }),
    [tokensPeriod, tokensChartData.labels, isDark, isMobile]
  );

  const availableModels = useMemo(
    () =>
      Array.from(
        new Set([
          ...Object.keys(requestsSeries?.dataByModel ?? {}),
          ...Object.keys(tokensSeries?.dataByModel ?? {})
        ])
      ).sort((left, right) => left.localeCompare(right)),
    [requestsSeries, tokensSeries]
  );

  return {
    requestsPeriod,
    setRequestsPeriod,
    tokensPeriod,
    setTokensPeriod,
    availableModels,
    requestsChartData,
    tokensChartData,
    requestsChartOptions,
    tokensChartOptions
  };
}
