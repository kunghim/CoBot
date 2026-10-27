import { useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { Bot, WorkspaceChangeFile, WorkspaceScope } from '../../api/types';
import DiffView from '../../components/DiffView';
import { Alert, Button, EmptyState, IconButton, Icons, Select, Spinner, SplitHandle, List, ListRow } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { openWorkspaceFile, scopeKey } from './fileNavigation';
import type { WorkspaceTab } from './state';
import { useWorkspace } from './WorkspaceProvider';

export default function ChangesTab({ scope, tab, bots }: { scope: WorkspaceScope; tab: WorkspaceTab; bots: Bot[] }) {
  const { t, i18n } = useTranslation();
  const { botId: currentBot, updateTab } = useWorkspace();
  const client = useQueryClient();
  const host = useRef<HTMLDivElement>(null);
  const previousBot = useRef(currentBot);
  const activeRunButton = useRef<HTMLButtonElement>(null);
  const shownFilePage = useRef('');
  const [width, setWidth] = useState(640);
  const [runCursors, setRunCursors] = useState(['']);
  const [fileCursors, setFileCursors] = useState(['']);
  const [diffPage, setDiffPage] = useState(0);
  const [openError, setOpenError] = useState('');
  const view = tab.changesView ?? {};
  const member = scope.kind === 'chat' ? '' : bots.some((bot) => bot.id === view.botId) ? view.botId! : currentBot ?? bots[0]?.id ?? '';
  const onView = (changesView: NonNullable<WorkspaceTab['changesView']>) => updateTab(tab.id, { changesView });
  const key = scopeKey(scope);
  const live = (status?: string) => !!status && !['completed', 'failed', 'interrupted'].includes(status);
  const runCursor = runCursors[runCursors.length - 1];
  const fileCursor = fileCursors[fileCursors.length - 1];
  const runs = useQuery({ queryKey: ['workspace-changes-runs', key, member, runCursor], queryFn: ({ signal }) => api.workspaceChangeRuns(scope, member, runCursor, signal), enabled: scope.kind === 'chat' || !!member, retry: false, refetchInterval: (query) => runCursor ? false : query.state.data?.runs.some((run) => live(run.status)) ? 3000 : 30_000 });
  const runID = view.runId ?? runs.data?.runs[0]?.id ?? '';
  const files = useQuery({ queryKey: ['workspace-changes-files', key, runID, fileCursor], queryFn: ({ signal }) => api.workspaceChangeFiles(scope, runID, fileCursor, signal), enabled: !!runID, retry: false, refetchInterval: (query) => fileCursor ? false : live(query.state.data?.run.status) ? 3000 : 30_000 });
  const ownerMatches = scope.kind === 'chat' || files.data?.run.botId === member;
  const path = view.path ?? (ownerMatches ? files.data?.files[0]?.path : undefined) ?? '';
  const diff = useQuery({ queryKey: ['workspace-changes-diff', key, runID, path], queryFn: ({ signal }) => api.workspaceChangeDiff(scope, runID, path, signal), enabled: !!runID && !!path && ownerMatches, retry: false, refetchInterval: (query) => live(query.state.data?.run.status) ? 3000 : 30_000 });
  useEffect(() => {
    const element = host.current; if (!element) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(element); return () => observer.disconnect();
  }, []);
  useEffect(() => { setRunCursors(['']); }, [member]);
  useEffect(() => { setFileCursors(['']); setOpenError(''); }, [runID]);
  useEffect(() => { setDiffPage(0); setOpenError(''); }, [runID, path, diff.data?.diff]);
  useEffect(() => {
    if (currentBot && previousBot.current !== currentBot) onView({ botId: currentBot, runId: undefined, path: undefined });
    previousBot.current = currentBot;
  }, [currentBot]);
  useEffect(() => {
    if (!view.runId && runs.data?.runs[0]) onView({ runId: runs.data.runs[0].id });
    if (view.path && ownerMatches && files.data?.run.fileCount === 0) onView({ path: undefined });
    if (!view.path && files.data?.files[0] && ownerMatches) onView({ path: files.data.files[0].path });
    if (files.data && !ownerMatches) onView({ runId: undefined, path: undefined });
  }, [view.runId, view.path, runs.data, files.data, ownerMatches]);
  useEffect(() => {
    const page = `${runID}:${fileCursor}`;
    if (files.data?.run.id !== runID || shownFilePage.current === page) return;
    shownFilePage.current = page;
    activeRunButton.current?.scrollIntoView({ block: 'nearest' });
  }, [runID, fileCursor, files.data]);
  const narrow = width < 560;
  const listOpen = view.listOpen ?? (!narrow || !path);
  const listWidth = Math.min(view.listWidth ?? 240, Math.max(160, Math.min(400, width - 180)));
  const activeRun = ownerMatches ? files.data?.run : undefined;
  const visibleRuns = [...(runs.data?.runs ?? [])];
  if (activeRun && !visibleRuns.some((run) => run.id === activeRun.id)) visibleRuns.unshift(activeRun);
  const visibleFiles = [...(ownerMatches ? files.data?.files ?? [] : [])];
  const selected = diff.data?.file;
  if (selected && diff.data?.run.id === runID && !visibleFiles.some((file) => file.path === selected.path)) visibleFiles.unshift(selected);
  const lines = (diff.data?.diff ?? '').split('\n');
  const currentDiffPage = Math.min(diffPage, Math.max(0, Math.ceil(lines.length / 500) - 1));
  const refresh = () => { setRunCursors(['']); setFileCursors(['']); void client.invalidateQueries({ predicate: (query) => typeof query.queryKey[0] === 'string' && query.queryKey[0].startsWith('workspace-changes-') && query.queryKey[1] === key }); };
  const failure = (error: unknown, retry: () => void) => <EmptyState title={apiErrorMessage(t, error)} action={<Button size="sm" onClick={retry}>{t('workspace.files.retry')}</Button>} />;
  return <div ref={host} className="flex h-full min-h-0 flex-col">
    <div className="flex min-h-11 items-center gap-2 border-b border-neutral-200 px-2 dark:border-neutral-800">
      <IconButton size="sm" icon={listOpen ? Icons.panelLeftClose : Icons.panelLeftOpen} label={t(listOpen ? 'workspace.changes.collapseList' : 'workspace.changes.expandList')} onClick={() => onView({ listOpen: !listOpen })} />
      <div className="min-w-0 flex-1">{scope.kind === 'team' ? <Select size="sm" label={t('workspace.changes.member')} value={member} options={bots.map((bot) => ({ value: bot.id, label: bot.name }))} onChange={(botId) => onView({ botId, runId: undefined, path: undefined })} /> : <span className="text-xs">{t('workspace.changes.chat')}</span>}</div>
      <IconButton size="sm" icon={Icons.refresh} label={t('workspace.changes.refresh')} onClick={refresh} />
    </div>
    <div className="relative flex min-h-0 flex-1">
      {listOpen && <>
        <aside className="flex h-full shrink-0 flex-col bg-side" style={{ width: narrow ? '100%' : listWidth }} aria-label={t('workspace.changes.history')}>
          <div className="min-h-0 flex-1 overflow-auto p-2">
            {runs.isError ? failure(runs.error, () => { setRunCursors(['']); void runs.refetch(); }) : runs.isPending ? <Spinner label={t('common.loading')} /> : !visibleRuns.length ? <EmptyState title={t('workspace.changes.empty')} /> : visibleRuns.map((run) => <section key={run.id}>
              <Button ref={run.id === runID ? activeRunButton : undefined} variant="ghost" size="sm" aria-expanded={run.id === runID} className="h-auto w-full flex-col items-stretch gap-1 px-2 py-2 text-left text-xs" onClick={() => onView({ runId: run.id, path: undefined })}>
                <span className="flex w-full items-center justify-between gap-2"><time dateTime={run.startedAt}>{new Date(run.startedAt).toLocaleString(i18n.language)}</time><ChangeStats item={run} /></span>
                <span className="text-2xs text-neutral-500">{t('workspace.changes.fileCount', { count: run.fileCount })} · {t(`chat.status.${run.status}`, { defaultValue: t('chat.status.unknown') })}</span>
              </Button>
              {run.id === runID && (files.isError ? failure(files.error, () => void files.refetch()) : files.isPending ? <Spinner label={t('common.loading')} /> : <>
                {activeRun && (!activeRun.statsComplete || !activeRun.filesComplete) && <p className="px-2 py-1 text-2xs text-neutral-500">{t('workspace.changes.incomplete')}</p>}
                {visibleFiles.length ? <List>{visibleFiles.map((file) => <ChangeFileRow key={file.path} file={file} selected={file.path === path} onSelect={() => onView({ path: file.path, listOpen: narrow ? false : view.listOpen })} />)}</List> : <p className="p-2 text-xs text-neutral-500">{t('workspace.changes.noFiles')}</p>}
                {(fileCursors.length > 1 || files.data?.nextCursor) && <div className="flex justify-between py-1">
                  <Button size="sm" variant="ghost" disabled={fileCursors.length <= 1} onClick={() => setFileCursors((items) => items.slice(0, -1))}>{t('workspace.files.previousPage')}</Button>
                  <Button size="sm" variant="ghost" disabled={!files.data?.nextCursor} onClick={() => setFileCursors((items) => [...items, files.data!.nextCursor])}>{t('workspace.files.nextPage')}</Button>
                </div>}
              </>)}
            </section>)}
          </div>
          {(runCursors.length > 1 || runs.data?.nextCursor) && <div className="flex justify-between border-t border-neutral-200 p-1 dark:border-neutral-800">
            <Button size="sm" variant="ghost" disabled={runCursors.length <= 1} onClick={() => { setRunCursors((items) => items.slice(0, -1)); onView({ runId: undefined, path: undefined }); }}>{t('workspace.changes.newer')}</Button>
            <Button size="sm" variant="ghost" disabled={!runs.data?.nextCursor} onClick={() => { setRunCursors((items) => [...items, runs.data!.nextCursor]); onView({ runId: undefined, path: undefined }); }}>{t('workspace.changes.older')}</Button>
          </div>}
        </aside>
        {!narrow && <SplitHandle side="left" value={listWidth} min={160} max={Math.max(160, Math.min(400, width - 180))} label={t('workspace.changes.listWidth')} onChange={(listWidth) => onView({ listWidth })} />}
      </>}
      {(!narrow || !listOpen) && <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {!path ? <EmptyState icon={<Icons.diff size={22} aria-hidden />} title={t('workspace.changes.select')} /> : diff.isError ? failure(diff.error, () => void diff.refetch()) : diff.isPending ? <Spinner label={t('common.loading')} /> : <>
          <div className="flex items-center gap-2 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
            <span className="min-w-0 flex-1 truncate font-mono text-xs" title={selected?.movePath ? `${path} → ${selected.movePath}` : path}>{selected?.movePath ? `${path} → ${selected.movePath}` : path}</span>
            <Button size="sm" variant="ghost" onClick={() => { setOpenError(''); void openWorkspaceFile(scope, selected?.movePath || path).catch((error) => setOpenError(apiErrorMessage(t, error))); }}>{t('workspace.changes.openCurrent')}</Button>
          </div>
          <p className="px-3 py-1 text-2xs text-neutral-500">{t('workspace.changes.recorded', { date: new Date(diff.data.run.startedAt).toLocaleString(i18n.language) })}</p>
          {openError && <Alert tone="danger">{openError}</Alert>}
          {diff.data.diffTruncated && <Alert tone="warning">{t('workspace.changes.truncated')}</Alert>}
          <div className="min-h-0 flex-1 overflow-hidden">{diff.data.diff ? <DiffView diff={diff.data.diff} startLine={currentDiffPage * 500} maxLines={500} className="h-full rounded-none" /> : <EmptyState title={t(diff.data.diffTruncated ? 'workspace.changes.unstored' : 'workspace.changes.noTextDiff')} />}</div>
          {lines.length > 500 && <div className="flex items-center justify-between border-t border-neutral-200 px-2 py-1 text-xs dark:border-neutral-800"><Button size="sm" variant="ghost" disabled={!currentDiffPage} onClick={() => setDiffPage(currentDiffPage - 1)}>{t('workspace.files.previousPage')}</Button><span>{currentDiffPage + 1} / {Math.ceil(lines.length / 500)}</span><Button size="sm" variant="ghost" disabled={(currentDiffPage + 1) * 500 >= lines.length} onClick={() => setDiffPage(currentDiffPage + 1)}>{t('workspace.files.nextPage')}</Button></div>}
        </>}
      </div>}
    </div>
  </div>;
}

export function ChangeStats({ item }: { item: Pick<WorkspaceChangeFile, 'statsComplete' | 'additions' | 'deletions'> }) {
  const { t } = useTranslation();
  return item.statsComplete ? <span className="shrink-0 whitespace-nowrap font-mono text-2xs"><span className="text-emerald-700 dark:text-emerald-300">+{item.additions}</span>{' '}<span className="text-red-700 dark:text-red-300">−{item.deletions}</span></span> : <span className="shrink-0 text-2xs text-neutral-500">{t('workspace.changes.unknownStats')}</span>;
}

export function ChangeFileRow({ file, selected, onSelect }: { file: WorkspaceChangeFile; selected: boolean; onSelect: () => void }) {
  const { t } = useTranslation();
  return <ListRow selected={selected} onClick={onSelect} title={<span className="font-mono" title={file.movePath ? `${file.path} → ${file.movePath}` : file.path}>{file.movePath || file.path}</span>} description={<>{t(`workspace.changes.${file.movePath ? 'rename' : file.kind}`)}{file.binary ? ` · ${t('workspace.changes.binary')}` : ''}</>} trailing={<ChangeStats item={file} />} />;
}
