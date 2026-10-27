import { useTranslation } from 'react-i18next';
import { Button } from '../../components/ui';
export function DocumentToolbar({ zoom, pages, onZoom }: { zoom: number | null; pages?: number; onZoom: (value: number | null) => void }) {
  const { t } = useTranslation();
  return <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800">
    <Button size="sm" variant="ghost" onClick={() => onZoom(null)}>{t('workspace.files.fitImage')}</Button><Button size="sm" variant="ghost" onClick={() => onZoom(1)}>100%</Button><Button size="sm" variant="ghost" disabled={zoom !== null && zoom <= 0.25} onClick={() => onZoom(Math.max(0.25, (zoom ?? 1) / 1.25))}>{t('workspace.files.zoomOut')}</Button><Button size="sm" variant="ghost" disabled={zoom !== null && zoom >= 2} onClick={() => onZoom(Math.min(2, (zoom ?? 1) * 1.25))}>{t('workspace.files.zoomIn')}</Button>{zoom !== null && <span>{Math.round(zoom * 100)}%</span>}{pages !== undefined && <span>{t('workspace.files.documentPages', { count: pages })}</span>}
  </div>;
}
