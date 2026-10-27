import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { EmptyState, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import { presentationFrame } from './presentationFrame';
import { PresentationToolbar } from './PresentationToolbar';
type Metadata = { slides: Array<string | null>; width: number; height: number };

export default function PPTXPreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const host = useRef<HTMLDivElement>(null);
  const worker = useRef<Worker>();
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const request = useRef(0);
  const [metadata, setMetadata] = useState<Metadata>();
  const [svg, setSVG] = useState('');
  const [error, setError] = useState('');
  const [page, setPage] = useState(0);
  const [zoom, setZoom] = useState<number | null>(null);
  const [space, setSpace] = useState({ width: 640, height: 400 });
  const content = useQuery({ queryKey: ['workspace-pptx', scopeKey(scope), file.path, file.version], queryFn: async ({ signal }) => (await api.workspaceBytes(scope, file.path, signal, undefined, file.version)).arrayBuffer(), enabled: file.size <= 10 * 1024 * 1024, retry: false, gcTime: 0 });
  const html = useMemo(() => svg ? presentationFrame(svg) : '', [svg]);
  const timedOut = () => { worker.current?.terminate(); setError('timeout'); };
  useEffect(() => {
    if (!content.data) return;
    setMetadata(undefined); setSVG(''); setError(''); setPage(0);
    const instance = new Worker(new URL('./presentation.worker.ts', import.meta.url), { type: 'module' });
    worker.current = instance;
    timer.current = setTimeout(timedOut, 12000);
    instance.onerror = () => { clearTimeout(timer.current); setError('invalid'); instance.terminate(); };
    instance.onmessage = (event: MessageEvent<{ type: string; slides?: Array<string | null>; width?: number; height?: number; svg?: string; request?: number; limit?: boolean }>) => {
      if (event.data.type === 'loaded') { clearTimeout(timer.current); setMetadata({ slides: event.data.slides!, width: event.data.width!, height: event.data.height! }); }
      else if (event.data.type === 'slide' && event.data.request === request.current) { clearTimeout(timer.current); setSVG(event.data.svg!); }
      else if (event.data.type === 'error') { clearTimeout(timer.current); setError(event.data.limit ? 'limit' : 'invalid'); instance.terminate(); }
    };
    // Keep the query's buffer intact for remount/retry; the worker owns a copy.
    const buffer = content.data.slice(0); instance.postMessage({ type: 'load', buffer }, [buffer]);
    return () => { clearTimeout(timer.current); instance.terminate(); worker.current = undefined; };
  }, [content.data]);
  useEffect(() => {
    if (!metadata?.slides.length || !worker.current) return;
    setSVG(''); clearTimeout(timer.current); timer.current = setTimeout(timedOut, 12000);
    worker.current.postMessage({ type: 'slide', index: page, request: ++request.current });
  }, [metadata, page]);
  useEffect(() => {
    const element = host.current; if (!element) return;
    const observer = new ResizeObserver(([entry]) => setSpace({ width: entry.contentRect.width, height: entry.contentRect.height }));
    observer.observe(element); return () => observer.disconnect();
  }, [metadata, error]);
  if (file.size > 10 * 1024 * 1024) return <EmptyState title={t('workspace.files.officeTooLarge')} />;
  if (content.isError || error) return <EmptyState title={error ? t(error === 'limit' ? 'workspace.files.presentationLimit' : error === 'timeout' ? 'workspace.files.presentationTimeout' : 'workspace.files.unreadablePresentation') : apiErrorMessage(t, content.error)} />;
  if (!metadata) return <Spinner label={t('common.loading')} />;
  if (!metadata.slides.length) return <EmptyState title={t('workspace.files.emptyPresentation')} />;
  const scale = zoom ?? Math.min(1, Math.max(0.01, (space.width - 24) / metadata.width), Math.max(0.01, (space.height - 24) / metadata.height));
  return <div className="flex h-full min-h-0 flex-col">
    <PresentationToolbar slides={metadata.slides} page={page} zoom={zoom} onPage={setPage} onZoom={setZoom} />
    <p className="border-b border-neutral-200 px-3 py-1 text-xs text-neutral-500 dark:border-neutral-800">{t('workspace.files.presentationStatic')}</p>
    <div ref={host} className="min-h-0 flex-1 overflow-auto bg-sunk p-3">
      {!html ? <Spinner label={t('common.loading')} /> : <iframe title={t('workspace.files.slideTitle', { index: page + 1 })} sandbox="" referrerPolicy="no-referrer" srcDoc={html} style={{ width: metadata.width * scale, height: metadata.height * scale, colorScheme: 'light' }} className="mx-auto block max-w-none shrink-0 border-0 bg-white shadow-sm" />}
    </div>
  </div>;
}
