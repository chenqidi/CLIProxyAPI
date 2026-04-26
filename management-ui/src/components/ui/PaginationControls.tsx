import { useTranslation } from 'react-i18next';
import styles from './PaginationControls.module.scss';

interface PaginationControlsProps {
  currentPage: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  className?: string;
  disabled?: boolean;
}

const normalizePage = (page: number, totalPages: number): number => {
  if (!Number.isFinite(page)) return 1;
  const safeTotal = Math.max(1, Math.round(totalPages));
  return Math.max(1, Math.min(safeTotal, Math.round(page)));
};

type PageLinkItem = number | 'ellipsis-start' | 'ellipsis-end';

const buildPageItems = (currentPage: number, totalPages: number): PageLinkItem[] => {
  const safeTotal = Math.max(1, Math.round(totalPages));
  const safeCurrent = normalizePage(currentPage, safeTotal);

  if (safeTotal <= 7) {
    return Array.from({ length: safeTotal }, (_, index) => index + 1);
  }

  if (safeCurrent <= 4) {
    return [1, 2, 3, 4, 5, 'ellipsis-end', safeTotal];
  }

  if (safeCurrent >= safeTotal - 3) {
    return [1, 'ellipsis-start', safeTotal - 4, safeTotal - 3, safeTotal - 2, safeTotal - 1, safeTotal];
  }

  return [1, 'ellipsis-start', safeCurrent - 1, safeCurrent, safeCurrent + 1, 'ellipsis-end', safeTotal];
};

export function PaginationControls({
  currentPage,
  totalPages,
  onPageChange,
  className = '',
  disabled = false,
}: PaginationControlsProps) {
  const { t } = useTranslation();
  const safeTotalPages = Math.max(1, Math.round(totalPages));
  const safeCurrentPage = normalizePage(currentPage, safeTotalPages);
  const pageItems = buildPageItems(safeCurrentPage, safeTotalPages);

  const goToPage = (page: number) => {
    const nextPage = normalizePage(page, safeTotalPages);
    onPageChange(nextPage);
  };

  const isFirstPage = safeCurrentPage <= 1;
  const isLastPage = safeCurrentPage >= safeTotalPages;
  const controlsDisabled = disabled || safeTotalPages <= 1;
  const classes = [styles.pagination, className].filter(Boolean).join(' ');

  return (
    <nav className={classes} aria-label={t('pagination.navigation')}>
      <button
        type="button"
        className={styles.edgeLink}
        onClick={() => goToPage(1)}
        disabled={controlsDisabled || isFirstPage}
      >
        {t('pagination.first')}
      </button>
      <button
        type="button"
        className={styles.edgeLink}
        onClick={() => goToPage(safeCurrentPage - 1)}
        disabled={controlsDisabled || isFirstPage}
      >
        {t('pagination.prev')}
      </button>
      <span className={styles.pagePrefix}>{t('pagination.page_prefix')}</span>
      <ol className={styles.pageList}>
        {pageItems.map((item) => {
          if (typeof item !== 'number') {
            return (
              <li key={item} className={styles.ellipsis} aria-hidden="true">
                ...
              </li>
            );
          }

          const isCurrent = item === safeCurrentPage;
          return (
            <li key={item}>
              <button
                type="button"
                className={`${styles.pageLink} ${isCurrent ? styles.pageLinkActive : ''}`}
                aria-current={isCurrent ? 'page' : undefined}
                aria-label={t(isCurrent ? 'pagination.current_page_aria' : 'pagination.page_link_aria', {
                  page: item
                })}
                disabled={disabled || isCurrent}
                onClick={() => goToPage(item)}
              >
                {item}
              </button>
            </li>
          );
        })}
      </ol>
      <button
        type="button"
        className={styles.edgeLink}
        onClick={() => goToPage(safeCurrentPage + 1)}
        disabled={controlsDisabled || isLastPage}
      >
        {t('pagination.next')}
      </button>
      <button
        type="button"
        className={styles.edgeLink}
        onClick={() => goToPage(safeTotalPages)}
        disabled={controlsDisabled || isLastPage}
      >
        {t('pagination.last')}
      </button>
    </nav>
  );
}
