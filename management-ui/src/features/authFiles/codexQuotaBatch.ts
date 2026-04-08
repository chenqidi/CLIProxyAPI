import type { TFunction } from 'i18next';
import { CODEX_CONFIG } from '@/components/quota';
import type { AuthFileItem, CodexQuotaState } from '@/types';
import { getStatusFromError, isCodexFile, isRuntimeOnlyAuthFile } from '@/utils/quota';

const PRIMARY_WEEKLY_WINDOW_ID = 'weekly';
const LIMIT_REACHED_PERCENT = 100;

export type CodexQuotaRefreshResult = {
  states: Map<string, CodexQuotaState>;
  successCount: number;
  failedCount: number;
};

export function isCodexQuotaManagedFile(file: AuthFileItem): boolean {
  return isCodexFile(file) && !isRuntimeOnlyAuthFile(file);
}

const getPrimaryWeeklyWindow = (quota?: CodexQuotaState | null) => {
  if (!quota || quota.status !== 'success') return null;
  return quota.windows.find((window) => window.id === PRIMARY_WEEKLY_WINDOW_ID) ?? null;
};

export function isCodexPrimaryWeeklyLimitReached(quota?: CodexQuotaState | null): boolean {
  const window = getPrimaryWeeklyWindow(quota);
  if (!window || typeof window.usedPercent !== 'number') return false;
  return window.usedPercent >= LIMIT_REACHED_PERCENT;
}

export function hasCodexPrimaryWeeklyQuotaAvailable(quota?: CodexQuotaState | null): boolean {
  const window = getPrimaryWeeklyWindow(quota);
  if (!window || typeof window.usedPercent !== 'number') return false;
  return window.usedPercent < LIMIT_REACHED_PERCENT;
}

export async function refreshCodexQuotaStates(
  files: AuthFileItem[],
  t: TFunction
): Promise<CodexQuotaRefreshResult> {
  const uniqueFiles = Array.from(
    new Map(
      files
        .filter((file) => isCodexQuotaManagedFile(file))
        .map((file) => [file.name, file])
    ).values()
  );

  const results = await Promise.all(
    uniqueFiles.map(async (file) => {
      try {
        const data = await CODEX_CONFIG.fetchQuota(file, t);
        return {
          name: file.name,
          ok: true as const,
          state: CODEX_CONFIG.buildSuccessState(data),
        };
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : t('common.unknown_error');
        const errorStatus = getStatusFromError(err);
        return {
          name: file.name,
          ok: false as const,
          state: CODEX_CONFIG.buildErrorState(message, errorStatus),
        };
      }
    })
  );

  const states = new Map<string, CodexQuotaState>();
  let successCount = 0;
  let failedCount = 0;

  results.forEach((result) => {
    states.set(result.name, result.state);
    if (result.ok) {
      successCount += 1;
    } else {
      failedCount += 1;
    }
  });

  return {
    states,
    successCount,
    failedCount,
  };
}
