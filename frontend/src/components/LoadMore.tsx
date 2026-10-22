import { useTranslation } from 'react-i18next';
import { Button } from './ui';

interface PageControl {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isFetchNextPageError: boolean;
  fetchNextPage: () => Promise<unknown>;
}

/** The next page of a list, on request; a failed page says so and retries. */
export default function LoadMore({ page }: { page: PageControl }) {
  const { t } = useTranslation();
  if (!page.hasNextPage && !page.isFetchNextPageError) return null;
  return (
    <div className="flex flex-col items-center gap-1 px-3 py-2">
      {page.isFetchNextPageError && <p role="alert" className="text-xs text-red-600 dark:text-red-400">{t('paging.failed')}</p>}
      <Button variant="ghost" size="sm" shape="pill" loading={page.isFetchingNextPage} onClick={() => void page.fetchNextPage()}>
        {t('paging.more')}
      </Button>
    </div>
  );
}