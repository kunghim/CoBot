import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { EmptyState, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import rendererURL from 'virtual:cobot-docx-renderer';
import { DocumentToolbar } from './DocumentToolbar';
import { docxFrame } from './docxFrame';

export default function DOCXPreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const frame = useRef<HTMLIFrameElement>(null);
  const [pages, setPages] = useState<number>();
  const [failed, setFailed] = useState(false);
  const [zoom, setZoom] = useState<number | null>(null);
  const content = useQuery({ queryKey: ['workspace-docx', scopeKey(scope), file.path, file.version], queryFn: async ({ signal }) => {
    const [blob, response] = await Promise.all([api.workspaceBytes(scope, file.path, signal, undefined, file.version), fetch(rendererURL, { signal })]);
    if (!response.ok) throw new Error('Renderer unavailable');
    return { buffer: await blob.arrayBuffer(), source: await response.text() };
  }, enabled: file.size <= 10 * 1024 * 1024, retry: false, gcTime: 0 });
  const html = useMemo(() => content.data ? docxFrame(content.data.source, crypto.randomUUID()) : '', [content.data]);
  useEffect(() => {
    if (!html) return;
    setPages(undefined); setFailed(false);
    const timer = setTimeout(() => setFailed(true), 15000);
    const receive = (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow) return;
      if (event.data?.type === 'docx-ready' && Number.isInteger(event.data.pages) && event.data.pages >= 0 && event.data.pages <= 200) { clearTimeout(timer); setPages(event.data.pages); }
      if (event.data?.type === 'docx-error') { clearTimeout(timer); setFailed(true); }
    };
    addEventListener('message', receive);
    return () => { clearTimeout(timer); removeEventListener('message', receive); };
  }, [html]);
  const scale = (value: number | null) => { setZoom(value); frame.current?.contentWindow?.postMessage({ type: 'zoom', value }, '*'); };
  if (file.size > 10 * 1024 * 1024) return <EmptyState title={t('workspace.files.officeTooLarge')} />;
  if (content.isError || failed) return <EmptyState title={failed || content.error?.message === 'Renderer unavailable' ? t('workspace.files.unreadableDOCX') : apiErrorMessage(t, content.error)} />;
  if (!content.data) return <Spinner label={t('common.loading')} />;
  return <div className="flex h-full min-h-0 flex-col">
    <DocumentToolbar zoom={zoom} pages={pages} onZoom={scale} />
    <p className="border-b border-neutral-200 px-3 py-1 text-xs text-neutral-500 dark:border-neutral-800">{t('workspace.files.documentLayout')}</p>
    {pages === undefined && <Spinner label={t('common.loading')} />}
    <iframe ref={frame} title={file.name} sandbox="allow-scripts" referrerPolicy="no-referrer" srcDoc={html} onLoad={() => frame.current?.contentWindow?.postMessage({ type: 'document', buffer: content.data.buffer }, '*')} className="min-h-0 w-full flex-1 border-0 bg-white" style={{ colorScheme: 'light' }} />
  </div>;
}
