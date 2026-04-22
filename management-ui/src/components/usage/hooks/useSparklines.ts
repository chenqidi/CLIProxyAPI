import { useCallback, useMemo } from 'react';
import {
  buildUsageChartCacheKey,
  getUsageTimeRangeHours,
  useUsageDashboardStore,
} from '@/stores';
import type { UsageChartData } from '@/services/api';
import type { UsageTimeRange } from '@/utils/usage';

export interface SparklineData {
  labels: string[];
  datasets: [
    {
      data: number[];
      borderColor: string;
      backgroundColor: string;
      fill: boolean;
      tension: number;
      pointRadius: number;
      borderWidth: number;
    }
  ];
}

export interface SparklineBundle {
  data: SparklineData;
}

export interface UseSparklinesOptions {
  timeRange: UsageTimeRange;
  loading: boolean;
}

export interface UseSparklinesReturn {
  requestsSparkline: SparklineBundle | null;
  tokensSparkline: SparklineBundle | null;
  rpmSparkline: SparklineBundle | null;
  tpmSparkline: SparklineBundle | null;
  costSparkline: SparklineBundle | null;
}

const aggregateChartData = (chart: UsageChartData | null) => {
  if (!chart) return { labels: [], values: [] };
  const labels = chart.labels;
  const series = Object.values(chart.dataByModel);
  const values = labels.map((_, idx) =>
    series.reduce((sum, modelSeries) => sum + (modelSeries[idx] ?? 0), 0)
  );
  return { labels, values };
};

export function useSparklines({ timeRange, loading }: UseSparklinesOptions): UseSparklinesReturn {
  const hours = getUsageTimeRangeHours(timeRange);
  const requestsKey = useMemo(
    () => buildUsageChartCacheKey({ range: timeRange, period: 'hour', metric: 'requests', hours }),
    [hours, timeRange]
  );
  const tokensKey = useMemo(
    () => buildUsageChartCacheKey({ range: timeRange, period: 'hour', metric: 'tokens', hours }),
    [hours, timeRange]
  );

  const requestsChart = useUsageDashboardStore((state) => state.chartCache[requestsKey]?.data ?? null);
  const tokensChart = useUsageDashboardStore((state) => state.chartCache[tokensKey]?.data ?? null);

  const requestSeries = useMemo(() => aggregateChartData(requestsChart), [requestsChart]);
  const tokenSeries = useMemo(() => aggregateChartData(tokensChart), [tokensChart]);

  const buildSparkline = useCallback(
    (
      series: { labels: string[]; data: number[] },
      color: string,
      backgroundColor: string
    ): SparklineBundle | null => {
      if (loading || !series?.data?.length) {
        return null;
      }
      const sliceStart = Math.max(series.data.length - 60, 0);
      const labels = series.labels.slice(sliceStart);
      const points = series.data.slice(sliceStart);
      return {
        data: {
          labels,
          datasets: [
            {
              data: points,
              borderColor: color,
              backgroundColor,
              fill: true,
              tension: 0.45,
              pointRadius: 0,
              borderWidth: 2
            }
          ]
        }
      };
    },
    [loading]
  );

  const rpmSeries = useMemo(
    () => ({
      labels: requestSeries.labels,
      data: requestSeries.values.map((value) => value / 60)
    }),
    [requestSeries.labels, requestSeries.values]
  );

  const tpmSeries = useMemo(
    () => ({
      labels: tokenSeries.labels,
      data: tokenSeries.values.map((value) => value / 60)
    }),
    [tokenSeries.labels, tokenSeries.values]
  );

  const requestsSparkline = useMemo(
    () =>
      buildSparkline(
        { labels: requestSeries.labels, data: requestSeries.values },
        '#8b8680',
        'rgba(139, 134, 128, 0.18)'
      ),
    [buildSparkline, requestSeries.labels, requestSeries.values]
  );

  const tokensSparkline = useMemo(
    () =>
      buildSparkline(
        { labels: tokenSeries.labels, data: tokenSeries.values },
        '#8b5cf6',
        'rgba(139, 92, 246, 0.18)'
      ),
    [buildSparkline, tokenSeries.labels, tokenSeries.values]
  );

  const rpmSparkline = useMemo(
    () => buildSparkline(rpmSeries, '#22c55e', 'rgba(34, 197, 94, 0.18)'),
    [buildSparkline, rpmSeries]
  );

  const tpmSparkline = useMemo(
    () => buildSparkline(tpmSeries, '#f97316', 'rgba(249, 115, 22, 0.18)'),
    [buildSparkline, tpmSeries]
  );

  return {
    requestsSparkline,
    tokensSparkline,
    rpmSparkline,
    tpmSparkline,
    costSparkline: null,
  };
}
