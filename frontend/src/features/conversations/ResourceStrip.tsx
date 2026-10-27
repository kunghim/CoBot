import { useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceScope } from '../../api/types';
import { Alert, Button, Icons, List, ListRow, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { ChangeStats } from '../workspace/ChangesTab';
import { openWorkspaceChanges } from '../workspace/changeNavigation';
import { openWorkspaceFile, scopeKey } from '../workspace/fileNavigation';
import { MarkdownLink } from './Markdown';
import { changedPaths, replyResources, resourcePathKey } from './resources';
import { currentFiles, resourceFiles, type ResourceFile } from './resourceFiles';

export default function ResourceStrip({ scope, run, text, directory = '' }: { scope: WorkspaceScope; run?: { id: string; status: string; steps: Array<{ kind: string; status: string; detail: Record<string, unknown> }> }; text: string; directory?: string }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const [sitePage, setSitePage] = useState(0);
  const [changeCursors, setChangeCursors] = useState(['']);
  const [resourcePages, setResourcePages] = useState<Array<{ start: number; seen: string[] }>>([{ start: 0, seen: [] }]);
  const [revision, setRevision] = useState(0);
  const [error, setError] = useState('');
  const previous = useRef(run?.steps);
  const settled = !run || ['completed', 'failed', 'interrupted'].includes(run.status);
  const resources = useMemo(() => replyResources(text), [text]);
  const paths = useMemo(() => changedPaths(run?.steps ?? []), [run?.steps]);
  const hasChanges = paths.length > 0 || !!run?.steps.some((step) => step.kind === 'diff' && step.status === 'completed' && typeof step.detail.diff === 'string' && step.detail.diff.trim());
  const key = scopeKey(scope);
  const changeCursor = changeCursors[changeCursors.length - 1];
  const changes = useQuery({ queryKey: ['reply-changes', key, run?.id, revision, ''], queryFn: ({ signal }) => api.workspaceChangeFiles(scope, run!.id, '', signal), enabled: settled && !!run && hasChanges, retry: false, staleTime: 'static' });
  const page = useQuery({ queryKey: ['reply-changes', key, run?.id, revision, changeCursor], queryFn: ({ signal }) => api.workspaceChangeFiles(scope, run!.id, changeCursor, signal), enabled: settled && !!run && hasChanges && expanded, retry: false, staleTime: 'static' });
  const current = useQuery({ queryKey: ['reply-changed-current', key, run?.id, revision, changeCursor], queryFn: ({ signal }) => currentFiles(scope, page.data!.files.map((file) => file.movePath || file.path), signal), enabled: expanded && !!page.data, retry: false, staleTime: 'static' });
  const changed = useMemo(() => [...new Set([...paths.map((path) => resourcePathKey(path, directory)), ...(changes.data?.files ?? []).flatMap((file) => [resourcePathKey(file.path, directory), ...(file.movePath ? [resourcePathKey(file.movePath, directory)] : [])]), ...(current.data ?? []).filter((file) => file !== null).map((file) => resourcePathKey(file.path))])].sort(), [paths, directory, changes.data, current.data]);
  const targets = useMemo(() => resources.filter((resource) => resource.kind === 'file').map((resource) => resource.target), [resources]);
  const sites = resources.filter((resource) => resource.kind === 'site');
  const cursor = resourcePages[resourcePages.length - 1];
  const files = useQuery({ queryKey: ['reply-files', key, targets, cursor, changed, run?.id, revision], queryFn: ({ signal }) => resourceFiles(scope, targets, cursor.start, cursor.seen, changed, hasChanges ? run?.id : undefined, signal), enabled: settled && targets.length > 0, retry: false, staleTime: 'static' });
  useEffect(() => {
    if (previous.current === run?.steps) return;
    previous.current = run?.steps;
    setRevision((value) => value + 1); setChangeCursors(['']); setResourcePages([{ start: 0, seen: [] }]);
  }, [run?.steps]);
  const action = (promise: Promise<void>) => { setError(''); void promise.catch((error) => setError(apiErrorMessage(t, error))); };
  const count = changes.data?.run.fileCount ?? 0;
  const visibleFiles = files.data?.files.filter((item) => !changed.includes(resourcePathKey(item.file.path))) ?? [];
  if (!settled || !count && !visibleFiles.length && !sites.length && resourcePages.length === 1) return null;
  return <section className="mt-3 space-y-2 border-t border-neutral-200 pt-2 dark:border-neutral-800" aria-label={t('workspace.resources.title')}>
    {error && <Alert tone="danger">{error}</Alert>}
    {count > 0 && <div>
      <div className="flex items-center gap-2">
        <Button variant="ghost" size="sm" icon={expanded ? Icons.expand : Icons.next} aria-expanded={expanded} onClick={() => setExpanded(!expanded)}><span>{t('workspace.resources.changed', { count })}</span><ChangeStats item={changes.data!.run} /></Button>
        <Button variant="link" size="sm" onClick={() => action(openWorkspaceChanges(scope, run!.id))}>{t('workspace.resources.reviewTurn')}</Button>
      </div>
      {expanded && (page.isPending ? <Spinner label={t('common.loading')} /> : page.isError ? <Alert tone="danger">{apiErrorMessage(t, page.error)}</Alert> : <>
        {(!page.data.run.statsComplete || !page.data.run.filesComplete) && <p className="py-1 text-xs text-neutral-500">{t('workspace.changes.incomplete')}</p>}
        <List>{page.data.files.map((file, index) => <ListRow key={file.path} title={<span className="font-mono text-xs" title={file.movePath ? `${file.path} → ${file.movePath}` : file.path}>{file.movePath || file.path}</span>} description={<ChangeStats item={file} />} trailing={<>
          <Button size="sm" variant="ghost" aria-label={`${t('workspace.resources.review')} ${file.path}`} onClick={() => action(openWorkspaceChanges(scope, run!.id, file.path))}>{t('workspace.resources.review')}</Button>
          <Button size="sm" variant="ghost" disabled={!current.data?.[index]} aria-label={`${t('workspace.resources.open')} ${file.movePath || file.path}`} onClick={() => action(openWorkspaceFile(scope, file.movePath || file.path))}>{t('workspace.resources.open')}</Button>
        </>} />)}</List>
        {(changeCursors.length > 1 || page.data.nextCursor) && <div className="flex justify-between"><Button size="sm" variant="ghost" disabled={changeCursors.length <= 1} onClick={() => setChangeCursors((items) => items.slice(0, -1))}>{t('workspace.files.previousPage')}</Button><Button size="sm" variant="ghost" disabled={!page.data.nextCursor} onClick={() => setChangeCursors((items) => [...items, page.data.nextCursor])}>{t('workspace.files.nextPage')}</Button></div>}
      </>)}
    </div>}
    {!!visibleFiles.length && <List>{visibleFiles.map((item) => <ReplyFileRow key={item.file.path} item={item} onOpen={() => action(openWorkspaceFile(scope, item.target))} />)}</List>}
    {(resourcePages.length > 1 || files.data && files.data.next >= 0) && <div className="flex justify-between"><Button size="sm" variant="ghost" disabled={resourcePages.length <= 1} onClick={() => setResourcePages((items) => items.slice(0, -1))}>{t('workspace.files.previousPage')}</Button><Button size="sm" variant="ghost" disabled={!files.data || files.data.next < 0} onClick={() => setResourcePages((items) => [...items, { start: files.data!.next, seen: [...cursor.seen, ...files.data!.files.map((item) => resourcePathKey(item.file.path))] }])}>{t('workspace.files.nextPage')}</Button></div>}
    {!!sites.length && <List>{sites.slice(sitePage * 20, (sitePage + 1) * 20).map((site) => <ListRow key={site.target} leading={<Icons.webSearch size={15} aria-hidden />} title={<span className="font-mono text-xs" title={site.target}>{site.target}</span>} trailing={<MarkdownLink href={site.target}>{t('workspace.resources.open')}</MarkdownLink>} />)}</List>}
    {sites.length > 20 && <div className="flex justify-between"><Button size="sm" variant="ghost" disabled={sitePage === 0} onClick={() => setSitePage((value) => value - 1)}>{t('workspace.files.previousPage')}</Button><Button size="sm" variant="ghost" disabled={(sitePage + 1) * 20 >= sites.length} onClick={() => setSitePage((value) => value + 1)}>{t('workspace.files.nextPage')}</Button></div>}
  </section>;
}

export function ReplyFileRow({ item, onOpen }: { item: ResourceFile; onOpen: () => void }) {
  const { t } = useTranslation();
  return <ListRow leading={<Icons.view size={15} aria-hidden />} title={<span className="font-mono text-xs" title={item.file.path}>{item.file.path || item.file.name}</span>} trailing={<Button size="sm" variant="ghost" onClick={onOpen} aria-label={`${t('workspace.resources.open')} ${item.file.path}`}>{t('workspace.resources.open')}</Button>} />;
}
