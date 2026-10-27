import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { EmptyState, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';

const MAX_MEDIA_BYTES = 128 * 1024 * 1024;

export default function MediaPreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const [url, setURL] = useState('');
  const [failed, setFailed] = useState(false);
  const media = useQuery({
    queryKey: ['workspace-media', scopeKey(scope), file.path, file.version],
    queryFn: ({ signal }) => api.workspaceBytes(scope, file.path, signal, undefined, file.version),
    enabled: file.size <= MAX_MEDIA_BYTES,
    retry: false,
    gcTime: 0,
  });
  useEffect(() => {
    setFailed(false);
    if (!media.data) { setURL(''); return; }
    const next = URL.createObjectURL(media.data);
    setURL(next);
    return () => URL.revokeObjectURL(next);
  }, [media.data]);
  if (file.size > MAX_MEDIA_BYTES) return <EmptyState title={t('workspace.files.mediaTooLarge')} />;
  if (media.isError) return <EmptyState title={apiErrorMessage(t, media.error)} />;
  if (failed) return <EmptyState title={t('workspace.files.unreadableMedia')} />;
  if (!url) return <Spinner label={t('common.loading')} />;
  return <MediaPlayer url={url} kind={file.kind === 'video' ? 'video' : 'audio'} name={file.name} onError={() => setFailed(true)} />;
}

export function MediaPlayer({ url, kind, name, onError }: { url: string; kind: 'audio' | 'video'; name: string; onError?: () => void }) {
  return <div className="flex h-full items-center justify-center overflow-auto p-4">
    {kind === 'video'
      ? <video key={url} src={url} controls playsInline preload="metadata" aria-label={name} onError={onError} className="max-h-full w-full object-contain" />
      : <audio key={url} src={url} controls preload="metadata" aria-label={name} onError={onError} className="w-full max-w-xl" />}
  </div>;
}
