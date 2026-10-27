import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { EmptyState, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import { prepareHTML, type HTMLResource } from './htmlDocument';

export default function HTMLPreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const [preview, setPreview] = useState<{ html: string; missing: boolean }>();
  const [error, setError] = useState<unknown>();
  const key = scopeKey(scope);
  const content = useQuery({ queryKey: ['workspace-html', key, file.path, file.version], queryFn: async ({ signal }) => (await api.workspaceBytes(scope, file.path, signal, undefined, file.version)).text(), enabled: file.size <= 1024 * 1024, retry: false, gcTime: 0 });
  useEffect(() => {
    setPreview(undefined); setError(undefined);
    if (content.data === undefined) return;
    const controller = new AbortController();
    const cache = new Map<string, Promise<string>>();
    let bytes = 0;
    let missing = false;
    const load: HTMLResource = (path, kind) => {
      const cacheKey = `${kind}:${path}`;
      const existing = cache.get(cacheKey);
      if (existing) return existing;
      const promise = (async () => {
        try {
          controller.signal.throwIfAborted();
          if (cache.size >= 64) throw new Error('Resource limit');
          const metadata = await api.workspaceFile(scope, path);
          controller.signal.throwIfAborted();
          const maximum = kind === 'style' ? 1024 * 1024 : 8 * 1024 * 1024;
          if (metadata.size > maximum || bytes + metadata.size > 20 * 1024 * 1024 || kind === 'image' && !['image', 'svg'].includes(metadata.kind) || kind === 'style' && !/\.css$/i.test(metadata.path) || kind === 'font' && !/\.(?:woff2?|ttf|otf)$/i.test(metadata.path)) throw new Error('Invalid resource');
          bytes += metadata.size;
          const blob = await api.workspaceBytes(scope, metadata.path, controller.signal, undefined, metadata.version);
          controller.signal.throwIfAborted();
          if (kind === 'style') return new TextDecoder('utf-8', { fatal: true }).decode(await blob.arrayBuffer());
          // An opaque sandbox cannot fetch Blob URLs owned by its parent origin.
          // Embed only the already validated, bounded bytes instead.
          const data = kind === 'font' ? new Blob([blob], { type: /\.woff2$/i.test(metadata.path) ? 'font/woff2' : /\.woff$/i.test(metadata.path) ? 'font/woff' : /\.otf$/i.test(metadata.path) ? 'font/otf' : 'font/ttf' }) : blob;
          return dataURL(data, controller.signal);
        } catch (error) { missing = true; throw error; }
      })();
      cache.set(cacheKey, promise);
      return promise;
    };
    prepareHTML(content.data, file.path, load).then((html) => { if (!controller.signal.aborted) setPreview({ html, missing }); }).catch((error) => { if (!controller.signal.aborted) setError(error); });
    return () => controller.abort();
  }, [content.data, key, file.path, file.version]);
  if (file.size > 1024 * 1024) return <EmptyState title={t('workspace.files.richTooLarge')} />;
  if (content.isError || error) return <EmptyState title={apiErrorMessage(t, content.error ?? error)} />;
  if (!preview) return <Spinner label={t('common.loading')} />;
  return <div className="flex h-full min-h-0 flex-col">
    <p className="border-b border-neutral-200 px-3 py-1 text-xs text-neutral-500 dark:border-neutral-800">{t(preview.missing ? 'workspace.files.htmlMissing' : 'workspace.files.htmlStatic')}</p>
    <HTMLFrame html={preview.html} title={file.name} />
  </div>;
}

export function HTMLFrame({ html, title }: { html: string; title: string }) {
  return <iframe style={{ colorScheme: 'light' }} sandbox="" referrerPolicy="no-referrer" title={title} srcDoc={html} className="min-h-0 w-full flex-1 border-0 bg-white" />;
}

function dataURL(blob: Blob, signal: AbortSignal): Promise<string> {
  return new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const reader = new FileReader();
    const abort = () => reader.abort();
    const done = () => signal.removeEventListener('abort', abort);
    reader.onload = () => { done(); resolve(String(reader.result)); };
    reader.onerror = () => { done(); reject(reader.error); };
    reader.onabort = () => { done(); reject(new DOMException('Aborted', 'AbortError')); };
    signal.addEventListener('abort', abort, { once: true });
    reader.readAsDataURL(blob);
  });
}
