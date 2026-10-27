import { useEffect, useRef, useState } from 'react';
import { TextLayer } from 'pdfjs-dist/legacy/build/pdf.mjs';
import type { PDFDocumentLoadingTask, PDFDocumentProxy, PDFPageProxy, RenderTask } from 'pdfjs-dist';
import { useTranslation } from 'react-i18next';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { Button, EmptyState, Spinner, TextInput } from '../../components/ui';
import { ApiError } from '../../api/client';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import { PDFSearchBar } from './PDFSearchBar';
import { searchPDF, type PDFSearchMatch } from './pdfSearch';
import { loadWorkspacePDF } from './pdfDocument';

export default function PDFPreview({ scope, file, data }: { scope: WorkspaceScope; file: WorkspaceFile; data?: Uint8Array }) {
  const { t } = useTranslation();
  const [pdf, setPDF] = useState<PDFDocumentProxy>();
  const [query, setQuery] = useState('');
  const [matches, setMatches] = useState<PDFSearchMatch[]>();
  const [matchIndex, setMatchIndex] = useState(0);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState(false);
  const [limited, setLimited] = useState(false);
  const search = useRef<AbortController>();
  const textDivs = useRef<HTMLElement[]>([]);
  const [error, setError] = useState<unknown>();
  const [page, setPage] = useState(1);
  const [zoom, setZoom] = useState<number | null>(null);
  const [scale, setScale] = useState(1);
  const [width, setWidth] = useState(600);
  const [rendering, setRendering] = useState(false);
  const [password, setPassword] = useState('');
  const [passwordRequest, setPasswordRequest] = useState<{ submit: (value: string) => void; incorrect: boolean }>();
  const host = useRef<HTMLDivElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);
  const text = useRef<HTMLDivElement>(null);
  const key = scopeKey(scope);
  useEffect(() => {
    const element = host.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    const controller = new AbortController();
    let loading: PDFDocumentLoadingTask | undefined;
    setPDF(undefined); setError(undefined); setPage(1); setPasswordRequest(undefined); setPassword('');
    if (!(data?.length ?? file.size)) { setError(new Error('Empty PDF')); return; }
    loadWorkspacePDF(scope, file, controller, data).then(({ task, range }) => {
      loading = task;
      if (controller.signal.aborted) { void task.destroy(); return; }
      if (range) range.failure = (error) => { if (!controller.signal.aborted) { setError(error); void task.destroy(); } };
      task.onPassword = (submit: (value: string) => void, reason: number) => { if (!controller.signal.aborted) setPasswordRequest({ submit, incorrect: reason === 2 }); };
      task.promise.then((document) => { if (!controller.signal.aborted) setPDF(document); }).catch((error) => { if (!controller.signal.aborted) { setError(error); void task.destroy(); } });
    }).catch((error) => { if (!controller.signal.aborted) setError(error); });
    return () => { controller.abort(); if (loading) void loading.destroy(); };
  }, [key, file.path, file.version, data]);
  useEffect(() => {
    if (!pdf || !canvas.current || !text.current) return;
    let active = true;
    let render: RenderTask | undefined;
    let pageDocument: PDFPageProxy | undefined;
    let layer: TextLayer | undefined;
    const surface = canvas.current;
    const content = text.current;
    setRendering(true);
    content.replaceChildren();
    pdf.getPage(page).then(async (document) => {
      if (!active) return;
      pageDocument = document;
      const natural = document.getViewport({ scale: 1 });
      if (!Number.isFinite(natural.width * natural.height) || natural.width <= 0 || natural.height <= 0) throw new Error('Invalid page');
      const ratio = Math.min(window.devicePixelRatio || 1, 2);
      const actual = Math.min(zoom ?? Math.max(0.05, (width - 32) / natural.width), 4, Math.sqrt(16_000_000 / (natural.width * natural.height * ratio * ratio)));
      const viewport = document.getViewport({ scale: actual });
      setScale(actual);
      surface.width = Math.ceil(viewport.width * ratio); surface.height = Math.ceil(viewport.height * ratio);
      surface.style.width = `${viewport.width}px`; surface.style.height = `${viewport.height}px`;
      content.style.width = `${viewport.width}px`; content.style.height = `${viewport.height}px`;
      content.style.setProperty('--total-scale-factor', String(actual));
      render = document.render({ canvas: surface, viewport, transform: [ratio, 0, 0, ratio, 0, 0] });
      layer = new TextLayer({ textContentSource: document.streamTextContent(), container: content, viewport });
      try { await Promise.all([render.promise, layer.render()]); }
      finally { if (!active) document.cleanup(); }
      if (active) { textDivs.current = layer.textDivs; setRendering(false); }
    }).catch((error) => { if (active) { setError(error); setRendering(false); } });
    return () => { active = false; render?.cancel(); layer?.cancel(); pageDocument?.cleanup(); content.replaceChildren(); };
  }, [pdf, page, zoom, width]);
  useEffect(() => {
    search.current?.abort(); setMatches(undefined); setSearching(false); setSearchError(false);
    return () => search.current?.abort();
  }, [pdf]);
  useEffect(() => {
    const content = text.current;
    if (!content || rendering) return;
    content.querySelectorAll('mark').forEach((mark) => { const parent = mark.parentNode; mark.replaceWith(document.createTextNode(mark.textContent ?? '')); parent?.normalize(); });
    const selected = matches?.[matchIndex];
    if (!selected || selected.page !== page) return;
    for (const part of selected.parts) {
      const span = textDivs.current[part.item]; if (!span) continue;
      const value = span.textContent ?? '';
      const mark = document.createElement('mark'); mark.textContent = value.slice(part.start, part.end);
      mark.style.backgroundColor = 'rgba(250, 204, 21, 0.4)'; mark.style.color = 'transparent';
      span.replaceChildren(document.createTextNode(value.slice(0, part.start)), mark, document.createTextNode(value.slice(part.end)));
    }
    content.querySelector('mark')?.scrollIntoView({ block: 'center', inline: 'nearest' });
  }, [matches, matchIndex, page, rendering]);
  const selectMatch = (index: number) => { if (matches?.[index]) { setMatchIndex(index); setPage(matches[index].page); } };
  const find = async () => {
    if (!pdf || !query) return;
    search.current?.abort(); const controller = new AbortController(); search.current = controller;
    setSearching(true); setSearchError(false); setMatches(undefined);
    try {
      const result = await searchPDF(pdf, query, controller.signal);
      if (controller.signal.aborted) return;
      setMatches(result.matches); setLimited(result.limited); setMatchIndex(0);
      if (result.matches.length) setPage(result.matches[0].page);
    } catch { if (!controller.signal.aborted) setSearchError(true); }
    finally { if (!controller.signal.aborted) setSearching(false); }
  };
  const failed = error instanceof ApiError ? apiErrorMessage(t, error) : t('workspace.files.unreadablePDF');
  return <div ref={host} className="flex h-full min-h-0 flex-col">
    {error ? <EmptyState title={failed} /> : passwordRequest ? <form className="m-auto flex max-w-sm flex-col gap-3 p-4" onSubmit={(event) => { event.preventDefault(); passwordRequest.submit(password); setPassword(''); setPasswordRequest(undefined); }}>
      <p className="text-sm">{t(passwordRequest.incorrect ? 'workspace.files.pdfPasswordIncorrect' : 'workspace.files.pdfPassword')}</p>
      <TextInput type="password" autoComplete="off" aria-label={t('workspace.files.pdfPassword')} value={password} onChange={(event) => setPassword(event.target.value)} />
      <Button type="submit" disabled={!password}>{t('workspace.files.pdfUnlock')}</Button>
    </form> : !pdf ? <Spinner label={t('common.loading')} /> : <>
      <PDFSearchBar query={query} searching={searching} count={matches?.length} current={matchIndex} limited={limited} error={searchError} onQuery={(value) => { search.current?.abort(); setSearching(false); setQuery(value); setMatches(undefined); setSearchError(false); }} onSearch={() => void find()} onCancel={() => { search.current?.abort(); setSearching(false); }} onSelect={selectMatch} />
      <PDFToolbar page={page} pages={pdf.numPages} scale={scale} onPage={setPage} onZoom={setZoom} />
      <div className="min-h-0 flex-1 overflow-auto bg-neutral-100 p-4 dark:bg-neutral-900" aria-busy={rendering}>
        <div className="relative mx-auto w-fit bg-white" style={{ colorScheme: 'light' }}><canvas ref={canvas} aria-label={file.name} /><div ref={text} className="pdf-text-layer" /></div>
      </div>
    </>}
  </div>;
}

export function PDFToolbar({ page, pages, scale, onPage, onZoom }: { page: number; pages: number; scale: number; onPage: (value: number) => void; onZoom: (value: number | null) => void }) {
  const { t } = useTranslation();
  return (
      <div className="flex flex-wrap items-center justify-between gap-1 border-b border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800">
        <div className="flex items-center gap-1"><Button size="sm" variant="ghost" disabled={page <= 1} onClick={() => onPage(page - 1)}>{t('workspace.files.previousPage')}</Button>
        <label className="flex items-center gap-1"><span>{t('workspace.files.pdfPage')}</span><TextInput size="sm" type="number" min={1} max={pages} aria-label={t('workspace.files.pdfPage')} value={page} onChange={(event) => { const next = Number(event.target.value); if (Number.isInteger(next) && next >= 1 && next <= pages) onPage(next); }} className="w-16" /> / {pages}</label>
        <Button size="sm" variant="ghost" disabled={page >= pages} onClick={() => onPage(page + 1)}>{t('workspace.files.nextPage')}</Button></div>
        <div className="flex items-center gap-1"><Button size="sm" variant="ghost" onClick={() => onZoom(null)}>{t('workspace.files.fitImage')}</Button>
        <Button size="sm" variant="ghost" disabled={scale <= 0.1} onClick={() => onZoom(Math.max(0.1, scale / 1.25))}>{t('workspace.files.zoomOut')}</Button>
        <span>{Math.round(scale * 100)}%</span>
        <Button size="sm" variant="ghost" disabled={scale >= 4} onClick={() => onZoom(Math.min(4, scale * 1.25))}>{t('workspace.files.zoomIn')}</Button></div>
      </div>
  );
}
