import { useCallback } from 'react';
import { useInterval } from '@/hooks/useInterval';
import { USAGE_STATUS_STALE_TIME_MS, useUsageStatusStore } from '@/stores';
import type { KeyStats, StatusBarData } from '@/utils/usage';

const EMPTY_KEY_STATS: KeyStats = { bySource: {}, byAuthIndex: {} };
const EMPTY_STATUS_BY_SOURCE: Record<string, StatusBarData> = {};

export type UseProviderStatsOptions = {
  enabled?: boolean;
};

export const useProviderStats = (options: UseProviderStatsOptions = {}) => {
  const enabled = options.enabled ?? true;
  const keyStats = useUsageStatusStore((state) => (enabled ? state.keyStats : EMPTY_KEY_STATS));
  const statusBySource = useUsageStatusStore((state) =>
    enabled ? state.statusBySource : EMPTY_STATUS_BY_SOURCE
  );
  const isLoading = useUsageStatusStore((state) => (enabled ? state.loading : false));
  const loadUsageStatus = useUsageStatusStore((state) => state.loadUsageStatus);

  // 首次进入页面优先复用缓存，避免跨页面重复拉取 /usage/status。
  const loadKeyStats = useCallback(async () => {
    await loadUsageStatus({ staleTimeMs: USAGE_STATUS_STALE_TIME_MS });
  }, [loadUsageStatus]);

  // 定时器触发时强制刷新共享 status 概览。
  const refreshKeyStats = useCallback(async () => {
    await loadUsageStatus({ force: true, staleTimeMs: USAGE_STATUS_STALE_TIME_MS });
  }, [loadUsageStatus]);

  useInterval(() => {
    void refreshKeyStats().catch(() => {});
  }, enabled ? 240_000 : null);

  return { keyStats, statusBySource, loadKeyStats, refreshKeyStats, isLoading };
};
