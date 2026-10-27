import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '../../api/client';
import type { WorkspaceScope } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { useTranslation } from 'react-i18next';
import { openWorkspaceFile, scopeKey } from './fileNavigation';

export default function LocalImage({ scope, path, alt }: { scope: WorkspaceScope; path: string; alt: string }) {
  const { t } = useTranslation();
  const [url, setURL] = useState('');
  const [error, setError] = useState('');
  const image = useQuery({ queryKey: ['workspace-inline-image', scopeKey(scope), path], queryFn: async ({ signal }) => {
    const file = await api.workspaceFile(scope, path);
    if (file.kind !== 'image' && file.kind !== 'svg') throw new Error('Not an image');
    return api.workspaceBytes(scope, file.path, signal, undefined, file.version);
  }, retry: false, gcTime: 30_000 });
  useEffect(() => {
    if (!image.data) { setURL(''); return; }
    const url = URL.createObjectURL(image.data); setURL(url);
    return () => URL.revokeObjectURL(url);
  }, [image.data]);
  if (image.isError) return <span role="status">{alt} · {apiErrorMessage(t, image.error)}</span>;
  return <span className="inline-block max-w-full">
    <button type="button" onClick={() => { void openWorkspaceFile(scope, path).catch((err) => setError(apiErrorMessage(t, err))); }} className="max-w-full rounded-lg" aria-label={alt || path}>
      {url ? <img src={url} alt={alt} className="max-h-96 max-w-full rounded-lg object-contain" /> : <span>{alt || t('common.loading')}</span>}
    </button>
    {error && <span role="alert" className="text-xs text-red-600 dark:text-red-400">{error}</span>}
  </span>;
}
