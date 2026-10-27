import { useEffect, useRef, useState } from 'react';
import { useQueries, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceScope } from '../../api/types';
import { Button, EmptyState, Icons, Spinner, Tree, type TreeNode } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import type { WorkspaceTab } from './state';
import { selectedChild } from './treeSelection';
import { scopeKey } from './fileNavigation';

type View = NonNullable<WorkspaceTab['fileView']>;
export default function FileTree({ scope, selected, view, onView, onSelect, onDirectories, refreshToken = 0 }: { scope: WorkspaceScope; selected: string; view: View; onView: (view: View) => void; onSelect: (path: string) => void; onDirectories?: (paths: string[]) => void; refreshToken?: number }) {
  const { t } = useTranslation();
  const host = useRef<HTMLDivElement>(null);
  const revealedElement = useRef<{ element: Element | null; count: number }>({ element: null, count: 0 });
  useEffect(() => {
    const element = host.current?.querySelector('[role=treeitem][aria-selected=true]') ?? null;
    const count = host.current?.querySelectorAll('[role=treeitem]').length ?? 0;
    if (element && (element !== revealedElement.current.element || count !== revealedElement.current.count)) element.scrollIntoView({ block: 'nearest' });
    revealedElement.current = { element, count };
  });
  const client = useQueryClient();
  const expanded = view.expanded ?? [];
  const [pages, setPages] = useState<Record<string, string[]>>({});
  useEffect(() => { setPages({}); }, [refreshToken]);
  const paths = [...new Set(['', ...expanded])];
  const requests = paths.flatMap((path) => ['', ...(pages[path] ?? [])].map((cursor) => ({ path, cursor })));
  const queries = useQueries({ queries: requests.map(({ path, cursor }) => ({
    queryKey: ['workspace-directory', scopeKey(scope), path, !!view.hidden, cursor],
    queryFn: ({ signal }: { signal: AbortSignal }) => api.workspaceEntries(scope, path, cursor, view.hidden, signal),
    staleTime: 5000,
    retry: false,
  })) });
  const directoryKey = JSON.stringify([...new Set(queries.flatMap((query, index) => requests[index].cursor === '' && query.isSuccess ? [query.data.path] : []))].sort());
  useEffect(() => { onDirectories?.(JSON.parse(directoryKey) as string[]); }, [directoryKey, onDirectories]);
  const missing = paths.flatMap((path) => {
    const indexes = requests.flatMap((request, index) => request.path === path ? [index] : []);
    const directory = queries[indexes[0]]?.data;
    if (!directory) return [];
    const candidate = selectedChild(directory.path, selected, indexes.flatMap((index) => queries[index].data?.entries.map((file) => file.name) ?? []));
    return candidate ? [{ branch: path, ...candidate }] : [];
  });
  const revealed = useQueries({ queries: missing.map((candidate) => ({
    queryKey: ['workspace-tree-selection', scopeKey(scope), candidate.path],
    queryFn: ({ signal }: { signal: AbortSignal }) => api.workspaceFile(scope, candidate.path, signal),
    retry: false,
    staleTime: 5000,
  })) });
  const branch = (path: string, ancestors = new Set<string>()): TreeNode[] => {
    const indexes = requests.flatMap((request, index) => request.path === path ? [index] : []);
    const canonical = indexes.length ? queries[indexes[0]].data?.path : undefined;
    if (canonical !== undefined && ancestors.has(canonical)) return [{ id: `\0cycle:${path}`, label: t('workspace.files.directoryCycle'), disabled: true }];
    const seen = new Set(ancestors);
    if (canonical !== undefined) seen.add(canonical);
    const entries = [...new Map(indexes.flatMap((index) => queries[index].data?.entries ?? []).map((file) => [file.name, file])).values()];
    const reveal = missing.findIndex((candidate) => candidate.branch === path);
    if (reveal >= 0 && revealed[reveal].data?.path === missing[reveal].path) entries.push({ ...revealed[reveal].data!, name: missing[reveal].name });
    const items: TreeNode[] = entries.map((file) => ({
      id: path ? `${path}/${file.name}` : file.name,
      label: file.name,
      directory: file.kind === 'directory',
      disabled: file.kind === 'blocked',
      leading: file.kind === 'directory' ? <Icons.openFolder size={14} aria-hidden /> : undefined,
      trailing: file.kind === 'blocked' ? <Icons.locked size={12} aria-hidden /> : undefined,
      children: file.kind === 'directory' && expanded.includes(path ? `${path}/${file.name}` : file.name) ? branch(path ? `${path}/${file.name}` : file.name, seen) : undefined,
    }));
    const last = indexes[indexes.length - 1];
    if (last !== undefined) {
      const query = queries[last];
      if (query.isPending) items.push({ id: `\0loading:${path}`, label: <Spinner label={t('common.loading')} />, disabled: true });
      else if (query.isError) items.push({ id: `\0retry:${path}`, label: t('workspace.files.retry'), trailing: <span title={apiErrorMessage(t, query.error)}><Icons.retry size={13} aria-hidden /></span> });
      else if (query.data?.nextCursor) items.push({ id: `\0more:${path}`, label: t('workspace.files.more') });
      else if (!entries.length && path) items.push({ id: `\0empty:${path}`, label: t('workspace.files.empty'), disabled: true });
    }
    return items;
  };
  const pick = (id: string) => {
    if (id.startsWith('\0retry:')) { void client.invalidateQueries({ queryKey: ['workspace-directory', scopeKey(scope), id.slice(7)] }); return; }
    if (id.startsWith('\0more:')) {
      const path = id.slice(6);
      const index = requests.map((request) => request.path).lastIndexOf(path);
      const cursor = queries[index]?.data?.nextCursor;
      if (cursor) setPages((previous) => ({ ...previous, [path]: [...(previous[path] ?? []), cursor] }));
      return;
    }
    onSelect(id);
  };
  const root = queries[0];
  return <div ref={host} className="flex h-full min-h-0 flex-col">
    <div className="flex items-center justify-between gap-2 px-2 py-2 text-xs font-semibold">
      <span>{t('workspace.files.tree')}</span>
      <Button variant="ghost" size="sm" title={t('workspace.files.refresh')} aria-label={t('workspace.files.refresh')} icon={Icons.refresh} onClick={() => { setPages({}); void client.invalidateQueries({ predicate: (query) => ['workspace-directory', 'workspace-tree-selection'].includes(String(query.queryKey[0])) && query.queryKey[1] === scopeKey(scope) }); }} />
    </div>
    <label className="flex items-center gap-2 px-3 pb-2 text-xs text-neutral-500"><input type="checkbox" checked={!!view.hidden} onChange={(event) => { setPages({}); onView({ hidden: event.target.checked }); }} />{t('workspace.files.hidden')}</label>
    <div className="min-h-0 flex-1 overflow-auto px-1 pb-3">
      {root?.isError ? <EmptyState title={apiErrorMessage(t, root.error)} action={<Button size="sm" onClick={() => void root.refetch()}>{t('workspace.files.retry')}</Button>} /> : root?.isPending ? <Spinner label={t('workspace.files.loading')} /> : root?.data?.entries.length === 0 ? <EmptyState title={t('workspace.files.empty')} /> : <Tree label={t('workspace.files.tree')} nodes={branch('')} expanded={expanded} onExpandedChange={(ids) => onView({ expanded: ids })} selected={selected} onSelect={pick} />}
    </div>
  </div>;
}
