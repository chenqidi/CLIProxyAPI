import type { CSSProperties, ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Line } from 'react-chartjs-2';
import {
  IconDiamond,
  IconSatellite,
  IconTimer,
  IconTrendingUp,
} from '@/components/ui/icons';
import { formatCompactNumber, formatPerMinuteValue } from '@/utils/usage';
import { sparklineOptions } from '@/utils/usage/chartConfig';
import type { UsageSummaryData } from '@/services/api';
import type { SparklineBundle } from './hooks/useSparklines';
import styles from '@/pages/UsagePage.module.scss';

interface StatCardData {
  key: string;
  label: string;
  icon: ReactNode;
  accent: string;
  accentSoft: string;
  accentBorder: string;
  value: string;
  meta?: ReactNode;
  trend: SparklineBundle | null;
}

export interface StatCardsProps {
  summary: UsageSummaryData | null;
  loading: boolean;
  sparklines: {
    requests: SparklineBundle | null;
    tokens: SparklineBundle | null;
    rpm: SparklineBundle | null;
    tpm: SparklineBundle | null;
  };
}

export function StatCards({ summary, loading, sparklines }: StatCardsProps) {
  const { t } = useTranslation();

  const statsCards: StatCardData[] = [
    {
      key: 'requests',
      label: t('usage_stats.total_requests'),
      icon: <IconSatellite size={16} />,
      accent: '#8b8680',
      accentSoft: 'rgba(139, 134, 128, 0.18)',
      accentBorder: 'rgba(139, 134, 128, 0.35)',
      value: loading ? '-' : (summary?.totalRequests ?? 0).toLocaleString(),
      meta: (
        <>
          <span className={styles.statMetaItem}>
            <span className={styles.statMetaDot} style={{ backgroundColor: '#10b981' }} />
            {t('usage_stats.success_requests')}: {loading ? '-' : (summary?.successCount ?? 0)}
          </span>
          <span className={styles.statMetaItem}>
            <span className={styles.statMetaDot} style={{ backgroundColor: '#c65746' }} />
            {t('usage_stats.failed_requests')}: {loading ? '-' : (summary?.failureCount ?? 0)}
          </span>
        </>
      ),
      trend: sparklines.requests
    },
    {
      key: 'tokens',
      label: t('usage_stats.total_tokens'),
      icon: <IconDiamond size={16} />,
      accent: '#8b5cf6',
      accentSoft: 'rgba(139, 92, 246, 0.18)',
      accentBorder: 'rgba(139, 92, 246, 0.35)',
      value: loading ? '-' : formatCompactNumber(summary?.totalTokens ?? 0),
      meta: (
        <>
          <span className={styles.statMetaItem}>
            {t('usage_stats.cached_tokens')}: {loading ? '-' : formatCompactNumber(summary?.cachedTokens ?? 0)}
          </span>
          <span className={styles.statMetaItem}>
            {t('usage_stats.reasoning_tokens')}: {loading ? '-' : formatCompactNumber(summary?.reasoningTokens ?? 0)}
          </span>
        </>
      ),
      trend: sparklines.tokens
    },
    {
      key: 'rpm',
      label: t('usage_stats.rpm_30m'),
      icon: <IconTimer size={16} />,
      accent: '#22c55e',
      accentSoft: 'rgba(34, 197, 94, 0.18)',
      accentBorder: 'rgba(34, 197, 94, 0.32)',
      value: loading ? '-' : formatPerMinuteValue(summary?.rpm30m ?? 0),
      meta: (
        <span className={styles.statMetaItem}>
          {t('usage_stats.total_requests')}: {loading ? '-' : (summary?.requestsLast30m ?? 0).toLocaleString()}
        </span>
      ),
      trend: sparklines.rpm
    },
    {
      key: 'tpm',
      label: t('usage_stats.tpm_30m'),
      icon: <IconTrendingUp size={16} />,
      accent: '#f97316',
      accentSoft: 'rgba(249, 115, 22, 0.18)',
      accentBorder: 'rgba(249, 115, 22, 0.32)',
      value: loading ? '-' : formatPerMinuteValue(summary?.tpm30m ?? 0),
      meta: (
        <span className={styles.statMetaItem}>
          {t('usage_stats.total_tokens')}: {loading ? '-' : formatCompactNumber(summary?.tokensLast30m ?? 0)}
        </span>
      ),
      trend: sparklines.tpm
    }
  ];

  return (
    <div className={styles.statsGrid}>
      {statsCards.map((card) => (
        <div
          key={card.key}
          className={styles.statCard}
          style={
            {
              '--accent': card.accent,
              '--accent-soft': card.accentSoft,
              '--accent-border': card.accentBorder
            } as CSSProperties
          }
        >
          <div className={styles.statCardHeader}>
            <div className={styles.statLabelGroup}>
              <span className={styles.statLabel}>{card.label}</span>
            </div>
            <span className={styles.statIconBadge}>{card.icon}</span>
          </div>
          <div className={styles.statValue}>{card.value}</div>
          {card.meta && <div className={styles.statMetaRow}>{card.meta}</div>}
          <div className={styles.statTrend}>
            {card.trend ? (
              <Line className={styles.sparkline} data={card.trend.data} options={sparklineOptions} />
            ) : (
              <div className={styles.statTrendPlaceholder}></div>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
