import { useTranslation } from 'react-i18next';
import { Button, Select } from '../../components/ui';
export function PresentationToolbar({ slides, page, zoom, onPage, onZoom }: { slides: Array<string | null>; page: number; zoom: number | null; onPage: (page: number) => void; onZoom: (zoom: number | null) => void }) {
  const { t } = useTranslation();
  return <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800">
    <Button size="sm" variant="ghost" disabled={page === 0} onClick={() => onPage(page - 1)}>{t('workspace.files.previousPage')}</Button>
    <div className="max-w-52"><Select size="sm" label={t('workspace.files.slide')} value={String(page)} options={slides.map((title, index) => ({ value: String(index), label: `${index + 1} / ${slides.length}${title ? ' · ' + title : ''}` }))} onChange={(value) => onPage(Number(value))} /></div>
    <Button size="sm" variant="ghost" disabled={page + 1 >= slides.length} onClick={() => onPage(page + 1)}>{t('workspace.files.nextPage')}</Button>
    <Button size="sm" variant="ghost" onClick={() => onZoom(null)}>{t('workspace.files.fitImage')}</Button><Button size="sm" variant="ghost" onClick={() => onZoom(1)}>100%</Button><Button size="sm" variant="ghost" disabled={zoom !== null && zoom <= 0.1} onClick={() => onZoom(Math.max(0.1, (zoom ?? 1) / 1.25))}>{t('workspace.files.zoomOut')}</Button><Button size="sm" variant="ghost" disabled={zoom !== null && zoom >= 3} onClick={() => onZoom(Math.min(3, (zoom ?? 1) * 1.25))}>{t('workspace.files.zoomIn')}</Button>{zoom !== null && <span>{Math.round(zoom * 100)}%</span>}
  </div>;
}
