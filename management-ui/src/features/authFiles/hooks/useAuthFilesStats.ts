import { useCallback } from 'react';
import { USAGE_STATUS_STALE_TIME_MS, useUsageStatusStore } from '@/stores';
import type { KeyStats, StatusBarData } from '@/utils/usage';

export type UseAuthFilesStatsResult = {
  keyStats: KeyStats;
  statusByAuthIndex: Record<string, StatusBarData>;
  loadKeyStats: () => Promise<void>;
  refreshKeyStats: () => Promise<void>;
};

export function useAuthFilesStats(): UseAuthFilesStatsResult {
  const keyStats = useUsageStatusStore((state) => state.keyStats);
  const statusByAuthIndex = useUsageStatusStore((state) => state.statusByAuthIndex);
  const loadUsageStatus = useUsageStatusStore((state) => state.loadUsageStatus);

  const loadKeyStats = useCallback(async () => {
    await loadUsageStatus({ staleTimeMs: USAGE_STATUS_STALE_TIME_MS });
  }, [loadUsageStatus]);

  const refreshKeyStats = useCallback(async () => {
    await loadUsageStatus({ force: true, staleTimeMs: USAGE_STATUS_STALE_TIME_MS });
  }, [loadUsageStatus]);

  return { keyStats, statusByAuthIndex, loadKeyStats, refreshKeyStats };
}
