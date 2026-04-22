import { useMemo } from 'react';
import type { AuthFileItem } from '@/types';
import { mergeStatusBarData, normalizeAuthIndex, type StatusBarData } from '@/utils/usage';

export type AuthFileStatusBarData = ReturnType<typeof mergeStatusBarData>;

export function useAuthFilesStatusBarCache(
  files: AuthFileItem[],
  statusByAuthIndex: Record<string, StatusBarData>
) {
  return useMemo(() => {
    const cache = new Map<string, AuthFileStatusBarData>();

    const uniqueAuthIndexKeys = new Set<string>();
    files.forEach((file) => {
      const rawAuthIndex = file['auth_index'] ?? file.authIndex;
      const authIndexKey = normalizeAuthIndex(rawAuthIndex);
      if (!authIndexKey) return;
      uniqueAuthIndexKeys.add(authIndexKey);
    });

    uniqueAuthIndexKeys.forEach((authIndexKey) => {
      cache.set(authIndexKey, statusByAuthIndex[authIndexKey] ?? mergeStatusBarData([]));
    });

    return cache;
  }, [files, statusByAuthIndex]);
}
