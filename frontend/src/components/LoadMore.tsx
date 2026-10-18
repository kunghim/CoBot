import { useTranslation } from 'react-i18next';

interface PageControl {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isFetchNextPageError: boolean;
  fetchNextPage: () => Promise<unknown>;
}

export default function LoadMore({ page }: { page: PageControl }) {
  const { t } = useTranslation();
  if (!page.hasNextPage && !page.isFetchNextPageError) return null;
  return <div className="px-3 py-2 text-center">
    {page.isFetchNextPageError && <p role="alert" className="mb-1 text-xs text-red-600">{t('paging.failed')}</p>}
    <button type="button" disabled={page.isFetchingNextPage} onClick={() => void page.fetchNextPage()} className="rounded-md border border-neutral-300 px-3 py-1.5 text-xs hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-800">{t(page.isFetchingNextPage ? 'status.loading' : 'paging.more')}</button>
  </div>;
}
