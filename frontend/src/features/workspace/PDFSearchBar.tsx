import { useTranslation } from 'react-i18next';
import { Button, TextInput } from '../../components/ui';
export function PDFSearchBar({ query, searching, count, current, limited, error, onQuery, onSearch, onCancel, onSelect }: { query: string; searching: boolean; count: number | undefined; current: number; limited: boolean; error: boolean; onQuery: (value: string) => void; onSearch: () => void; onCancel: () => void; onSelect: (index: number) => void }) {
  const { t } = useTranslation();
  return <form aria-busy={searching} className="flex flex-wrap items-center gap-1 border-b border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800" onSubmit={(event) => { event.preventDefault(); onSearch(); }}>
    <TextInput size="sm" className="min-w-24 flex-1" maxLength={256} aria-label={t('workspace.files.pdfFind')} placeholder={t('workspace.files.pdfFind')} value={query} onChange={(event) => onQuery(event.target.value)} />
    <Button size="sm" variant="ghost" type="submit" disabled={!query || searching}>{t('workspace.files.pdfFindAction')}</Button>
    {searching ? <Button size="sm" variant="ghost" onClick={onCancel}>{t('workspace.files.cancelSearch')}</Button> : <>
      <Button size="sm" variant="ghost" disabled={!count || current <= 0} onClick={() => onSelect(current - 1)}>{t('workspace.files.previousMatch')}</Button><Button size="sm" variant="ghost" disabled={!count || current + 1 >= count} onClick={() => onSelect(current + 1)}>{t('workspace.files.nextMatch')}</Button>
      {count !== undefined && <span role="status">{t('workspace.files.pdfMatches', { current: count ? current + 1 : 0, count })}{limited ? '+' : ''}</span>}
    </>}
    <span className="text-neutral-500">{t(error ? 'workspace.files.pdfSearchError' : 'workspace.files.pdfFindHint')}</span>
  </form>;
}
