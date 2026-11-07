import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { Button, EmptyState, Spinner } from '../../components/ui';
import { scopeKey } from './fileNavigation';

export default function ImagePreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const [url, setURL] = useState('');
  const [failed, setFailed] = useState(false);
  const [zoom, setZoom] = useState<number | null>(null);
  const [dimensions, setDimensions] = useState({ width: 0, height: 0 });
  const image = useQuery({ queryKey: ['workspace-image', scopeKey(scope), file.path, file.version], queryFn: ({ signal }) => api.workspaceBytes(scope, file.path, signal, undefined, file.version), retry: false, gcTime: 30_000 });
  useEffect(() => {
    setFailed(false);
    if (!image.data) { setURL(''); return; }
    const url = URL.createObjectURL(image.data); setURL(url);
    return () => URL.revokeObjectURL(url);
  }, [image.data]);
  if (image.isError || failed) return <EmptyState title={t('workspace.files.unreadableImage')} />;
  if (!url) return <Spinner label={t('common.loading')} />;
  return <div className="flex h-full min-h-0 flex-col">
    <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800">
      <span>{dimensions.width} × {dimensions.height}</span>
      <Button size="sm" variant="ghost" onClick={() => setZoom(null)}>{t('workspace.files.fitImage')}</Button>
      <Button size="sm" variant="ghost" onClick={() => setZoom(1)}>100%</Button>
      <Button size="sm" variant="ghost" disabled={zoom !== null && zoom <= 0.1} onClick={() => setZoom((value) => Math.max(0.1, (value ?? 1) / 1.25))}>{t('workspace.files.zoomOut')}</Button>
      <Button size="sm" variant="ghost" disabled={zoom !== null && zoom >= 8} onClick={() => setZoom((value) => Math.min(8, (value ?? 1) * 1.25))}>{t('workspace.files.zoomIn')}</Button>
      {zoom !== null && <span>{Math.round(zoom * 100)}%</span>}
    </div>
    <div className="min-h-0 flex-1 overflow-auto p-4">
      <img alt={file.name} src={url} onLoad={(event) => setDimensions({ width: event.currentTarget.naturalWidth, height: event.currentTarget.naturalHeight })} onError={() => setFailed(true)} className={zoom === null ? 'mx-auto max-h-full max-w-full object-contain' : 'max-w-none'} style={zoom === null ? undefined : { width: dimensions.width * zoom, height: dimensions.height * zoom }} />
    </div>
  </div>;
}
