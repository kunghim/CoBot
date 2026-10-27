import { useEffect, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { Button, EmptyState, Spinner, TextInput, cx } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import { SOURCE_CHUNK_BYTES, sourcePage } from './sourcePages';
import { highlightLines, type SourceToken } from './sourceHighlight';
import { searchSource } from './sourceSearch';
import type { WorkspaceTab } from './state';

type View = NonNullable<WorkspaceTab['fileView']>;
export default function SourcePreview({ scope, file, view, onView }: { scope: WorkspaceScope; file: WorkspaceFile; view: View; onView: (view: View) => void }) {
  const { t } = useTranslation();
  const [find, setFind] = useState('');
  const [search, setSearch] = useState('');
  const [matchIndex, setMatchIndex] = useState(0);
  useEffect(() => { const timer = setTimeout(() => { setSearch(find); setMatchIndex(0); }, 300); return () => clearTimeout(timer); }, [find]);
  const results = useQuery({
    queryKey: ['workspace-search', scopeKey(scope), file.path, file.version, search],
    enabled: !!search,
    retry: false,
    gcTime: 0,
    queryFn: ({ signal }) => searchSource((async function* () {
      let position = 0;
      while (position < file.size) {
        signal.throwIfAborted();
        const blob = await api.workspaceBytes(scope, file.path, signal, `bytes=${position}-${Math.min(file.size - 1, position + SOURCE_CHUNK_BYTES - 1)}`, file.version);
        const chunk = sourcePage(new Uint8Array(await blob.arrayBuffer()), position + blob.size >= file.size);
        if (!chunk.consumed) break;
        position += chunk.consumed;
        yield chunk.text;
      }
    })(), search),
  });
  const selectMatch = (index: number) => {
    const match = results.data?.matches[index];
    if (!match) return;
    setMatchIndex(index);
    onView({ offset: 0, startLine: 1, line: match.line, column: match.column, endLine: undefined, positionId: (view.positionId ?? 0) + 1, scrollTop: 0 });
  };
  const [wrap, setWrap] = useState(false);
  const [history, setHistory] = useState<Array<{ offset: number; startLine: number }>>([]);
  const scroll = useRef<HTMLDivElement>(null);
  const saveScroll = useRef<ReturnType<typeof setTimeout>>();
  useEffect(() => () => clearTimeout(saveScroll.current), []);
  const offset = view.offset && view.offset < file.size ? view.offset : 0;
  const startLine = offset ? view.startLine ?? 1 : 1;
  const page = useQuery({
    queryKey: ['workspace-source', scopeKey(scope), file.path, file.version, offset],
    queryFn: async ({ signal }) => {
      if (!file.size) return { text: '', consumed: 0, newlines: 0 };
      const blob = await api.workspaceBytes(scope, file.path, signal, `bytes=${offset}-${Math.min(file.size - 1, offset + SOURCE_CHUNK_BYTES - 1)}`, file.version);
      return sourcePage(new Uint8Array(await blob.arrayBuffer()), offset + blob.size >= file.size);
    },
    retry: false,
    gcTime: 30_000,
  });
  const [highlight, setHighlight] = useState<{ text: string; path: string; lines: SourceToken[][] }>();
  const text = page.data?.text;
  useEffect(() => {
    if (!text || typeof Worker === 'undefined') return;
    const worker = new Worker(new URL('./sourceHighlight.worker.ts', import.meta.url), { type: 'module' });
    const timeout = setTimeout(() => worker.terminate(), 1500);
    worker.onmessage = (event: MessageEvent<string>) => {
      clearTimeout(timeout);
      if (typeof event.data === 'string' && event.data) {
        const lines = highlightLines(event.data);
        if (lines.map((tokens) => tokens.map((token) => token.text).join('')).join('\n') === text.replace(/\r\n|\r/g, '\n')) setHighlight({ text, path: file.path, lines });
      }
      worker.terminate();
    };
    worker.onerror = () => { clearTimeout(timeout); worker.terminate(); };
    worker.postMessage({ text, path: file.path });
    return () => { clearTimeout(timeout); worker.terminate(); };
  }, [text, file.path]);
  const nextOffset = offset + (page.data?.consumed ?? 0);
  const nextLine = startLine + (page.data?.newlines ?? 0);
  const hasNext = nextOffset > offset && nextOffset < file.size;
  const lines = (page.data?.text.replace(/(?:\r\n|\r|\n)$/, '') ?? '').split(/\r\n|\r|\n/);
  useEffect(() => {
    if (!page.data) return;
    if (view.line && view.line > startLine + lines.length - 1 && hasNext) {
      onView({ offset: nextOffset, startLine: nextLine, scrollTop: 0 });
      return;
    }
    const target = view.line ? scroll.current?.querySelector<HTMLElement>(`[data-line="${view.line}"]`) : null;
    if (target) {
      target.scrollIntoView({ block: 'center', inline: 'nearest' });
      if (view.column && scroll.current) revealColumn(scroll.current, target, view.column);
    }
    else if (scroll.current) scroll.current.scrollTop = view.scrollTop ?? 0;
  }, [page.data, view.line, view.positionId]);
  if (page.isError) return <EmptyState title={apiErrorMessage(t, page.error)} action={<Button size="sm" onClick={() => void page.refetch()}>{t('workspace.files.retry')}</Button>} />;
  if (page.isPending) return <Spinner label={t('common.loading')} />;
  const mark = (line: string, index: number) => {
    const tokens = highlight && highlight.text === text && highlight.path === file.path ? highlight.lines[index] : undefined;
    const match = find ? line.indexOf(find) : -1;
    let offset = 0;
    return (tokens?.length ? tokens : [{ text: line || ' ', classes: '' }]).map((token, index) => {
      const start = offset; offset += token.text.length;
      const from = Math.max(0, match - start);
      const to = Math.min(token.text.length, match + find.length - start);
      return <span key={index} className={token.classes || undefined}>{match >= 0 && from < to ? <>{token.text.slice(0, from)}<mark className="rounded bg-amber-200 text-neutral-900">{token.text.slice(from, to)}</mark>{token.text.slice(to)}</> : token.text}</span>;
    });
  };
  return <div className="flex h-full min-h-0 flex-col">
    <div className="flex items-center gap-2 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
      <TextInput size="sm" aria-label={t('workspace.files.search')} placeholder={t('workspace.files.search')} value={find} onChange={(event) => setFind(event.target.value)} />
      <label className="flex shrink-0 items-center gap-1 text-xs"><input type="checkbox" checked={wrap} onChange={(event) => setWrap(event.target.checked)} />{t('workspace.files.wrap')}</label>
    </div>
    {find && <div className="flex items-center gap-2 border-b border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800" aria-live="polite">
      {results.isFetching || search !== find ? <Spinner label={t('common.loading')} /> : results.isError ? <span>{apiErrorMessage(t, results.error)}</span> : <>
        <span>{t('workspace.files.matches', { count: results.data?.matches.length ?? 0 })}{results.data?.limited ? '+' : ''}</span>
        <Button size="sm" variant="ghost" disabled={!results.data?.matches.length} onClick={() => selectMatch((matchIndex + (results.data?.matches.length ?? 1) - 1) % (results.data?.matches.length ?? 1))}>{t('workspace.files.previousMatch')}</Button>
        <Button size="sm" variant="ghost" disabled={!results.data?.matches.length} onClick={() => selectMatch(matchIndex % (results.data?.matches.length ?? 1))}>{t('workspace.files.goToMatch', { index: matchIndex + 1 })}</Button>
        <Button size="sm" variant="ghost" disabled={!results.data?.matches.length} onClick={() => selectMatch((matchIndex + 1) % (results.data?.matches.length ?? 1))}>{t('workspace.files.nextMatch')}</Button>
      </>}
    </div>}
    <div ref={scroll} onScroll={() => { const top = scroll.current?.scrollTop; clearTimeout(saveScroll.current); if (top !== undefined) saveScroll.current = setTimeout(() => onView({ scrollTop: top }), 100); }} className="min-h-0 flex-1 source-highlight overflow-auto py-2 font-mono text-xs leading-5" aria-label={file.path}>
      {lines.map((line, index) => <div key={index} data-line={startLine + index} className={cx('flex min-w-full', !!view.line && startLine + index >= view.line && startLine + index <= (view.endLine ?? view.line) && 'bg-signal-500/10')}>
        <span aria-hidden className="sticky left-0 w-12 shrink-0 select-none bg-canvas pr-3 text-right text-neutral-400">{startLine + index}</span>
        <code className={cx('block min-w-0 flex-1 pr-4', wrap ? 'whitespace-pre-wrap break-all' : 'whitespace-pre')}>{mark(line, index)}</code>
      </div>)}
    </div>
    <div className="flex items-center justify-between gap-2 border-t border-neutral-200 px-3 py-1 text-xs text-neutral-500 dark:border-neutral-800">
      <Button variant="ghost" size="sm" disabled={!offset} onClick={() => { const previous = history[history.length - 1] ?? { offset: 0, startLine: 1 }; setHistory((items) => items.slice(0, -1)); onView({ ...previous, line: undefined, scrollTop: 0 }); }}>{t('workspace.files.previousPage')}</Button>
      <span>{t('workspace.files.page', { start: startLine, end: startLine + lines.length - 1 })}</span>
      <Button variant="ghost" size="sm" disabled={!hasNext} onClick={() => { setHistory((items) => [...items.slice(-100), { offset, startLine }]); onView({ offset: nextOffset, startLine: nextLine, line: undefined, scrollTop: 0 }); }}>{t('workspace.files.nextPage')}</Button>
    </div>
  </div>;
}

function revealColumn(container: HTMLElement, line: HTMLElement, column: number): void {
  const code = line.querySelector('code');
  if (!code) return;
  const walker = document.createTreeWalker(code, NodeFilter.SHOW_TEXT);
  let remaining = Math.max(0, column - 1);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const length = node.textContent?.length ?? 0;
    if (remaining > length) { remaining -= length; continue; }
    const range = document.createRange();
    range.setStart(node, remaining); range.setEnd(node, Math.min(length, remaining + 1));
    const target = range.getBoundingClientRect();
    const viewport = container.getBoundingClientRect();
    if (target.left < viewport.left + 48 || target.right > viewport.right) container.scrollLeft += target.left - viewport.left - 72;
    break;
  }
}
