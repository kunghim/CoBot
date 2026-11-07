import { Suspense, useCallback, useEffect, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { Alert, Button, CopyButton, EmptyState, IconButton, Icons, Spinner, SplitHandle } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import FileTree from './FileTree';
import type { WorkspaceTab } from './state';
import { useWorkspaceWatch } from './useWorkspaceWatch';
import { pickViewer, showsSourceButton } from './viewers';
import { useWorkspace } from './WorkspaceProvider';

type View = NonNullable<WorkspaceTab['fileView']>;
export default function FileTab({ tab, scope }: { tab: WorkspaceTab; scope: WorkspaceScope }) {
  const { t } = useTranslation();
  const { updateTab } = useWorkspace();
  const client = useQueryClient();
  const host = useRef<HTMLDivElement>(null);
  const lastLocation = useRef('');
  const request = useRef(0);
  const [width, setWidth] = useState(640);
  const [error, setError] = useState('');
  const [previewRefresh, setPreviewRefresh] = useState(0);
  const [directories, setDirectories] = useState<string[]>([]);
  const [treeRefresh, setTreeRefresh] = useState(0);
  const onDirectories = useCallback((paths: string[]) => setDirectories((previous) => JSON.stringify(previous) === JSON.stringify(paths) ? previous : paths), []);
  const view = tab.fileView ?? {};
  const file = useQuery({ queryKey: ['workspace-file', scopeKey(scope), tab.ref], queryFn: () => api.workspaceFile(scope, tab.ref), retry: false, refetchInterval: 3000 });
  const parent = file.data?.kind === 'directory' ? file.data.path : file.data?.path.split('/').slice(0, -1).join('/') ?? '';
  const watchUnavailable = useWorkspaceWatch(scope, ['', parent, ...directories], previewRefresh, (resetPages) => {
    if (resetPages) setTreeRefresh((value) => value + 1);
    void client.invalidateQueries({ predicate: (query) => typeof query.queryKey[0] === 'string' && query.queryKey[0].startsWith('workspace-') && query.queryKey[1] === scopeKey(scope) });
  });
  const onView = (patch: View) => updateTab(tab.id, { fileView: patch });
  useEffect(() => {
    const element = host.current;
    if (!element) return;
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width));
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (!file.data) return;
    const location = `${tab.id}:${tab.ref}:${file.data.kind}`;
    if (lastLocation.current === location) return;
    lastLocation.current = location;
    const parts = file.data.path.split('/');
    const count = file.data.kind === 'directory' ? parts.length : parts.length - 1;
    const ancestors = Array.from({ length: count }, (_, index) => parts.slice(0, index + 1).join('/')).filter(Boolean);
    const expanded = [...new Set([...(view.expanded ?? []), ...ancestors])];
    updateTab(tab.id, { ref: file.data.path, title: file.data.path ? file.data.name : t('workspace.files.title'), fileView: { expanded } });
  }, [file.data, tab.id, tab.ref, updateTab, view.expanded]);
  const select = async (path: string) => {
    const sequence = ++request.current;
    setError('');
    try {
      const selected = await api.workspaceFile(scope, path);
      if (sequence !== request.current) return;
      updateTab(tab.id, { ref: selected.path, title: selected.name, fileView: { line: undefined, endLine: undefined, column: undefined, offset: 0, startLine: 1, scrollTop: 0 } });
    } catch (err) { if (sequence === request.current) setError(apiErrorMessage(t, err)); }
  };
  const external = (reveal: boolean) => {
    setError('');
    (scope.kind === 'chat' ? api.openChatPath(scope.id, tab.ref, reveal) : api.openTeamPath(scope.id, tab.ref, reveal)).catch((err) => setError(apiErrorMessage(t, err)));
  };
  const treeOpen = view.treeOpen ?? width >= 560;
  const maxTree = Math.max(140, Math.min(400, width - 180));
  const treeWidth = Math.min(view.treeWidth ?? 200, maxTree);
  return <div ref={host} className="flex h-full min-h-0 flex-col">
    <div className="flex min-h-10 items-center gap-1 border-b border-neutral-200 px-2 dark:border-neutral-800">
      <IconButton size="sm" icon={treeOpen ? Icons.panelLeftClose : Icons.panelLeftOpen} label={t(treeOpen ? 'workspace.files.collapseTree' : 'workspace.files.expandTree')} onClick={() => onView({ treeOpen: !treeOpen })} />
      <span className="min-w-0 flex-1 truncate font-mono text-xs" title={tab.ref}>{tab.ref || t('workspace.files.title')}</span>
      {tab.ref && <CopyButton value={tab.ref} copyLabel={t('chat.copy')} copiedLabel={t('chat.copied')} />}
      <IconButton size="sm" icon={Icons.openExternal} label={t('workspace.files.system')} disabled={!tab.ref} onClick={() => external(false)} />
      <IconButton size="sm" icon={Icons.openFolder} label={t('workspace.files.reveal')} disabled={!tab.ref} onClick={() => external(true)} />
      <IconButton size="sm" icon={Icons.refresh} label={t('workspace.files.refresh')} onClick={() => { void client.invalidateQueries({ predicate: (query) => typeof query.queryKey[0] === 'string' && query.queryKey[0].startsWith('workspace-') && query.queryKey[1] === scopeKey(scope) }).then(() => setPreviewRefresh((value) => value + 1)); }} />
    </div>
    {watchUnavailable && <p role="status" className="border-b border-neutral-200 px-3 py-1 text-xs text-neutral-500 dark:border-neutral-800">{t('errors.error.workspace.watch_unavailable')}</p>}
    {error && <Alert tone="danger">{error}</Alert>}
    <div className="relative flex min-h-0 flex-1">
      {treeOpen && <>
        <aside style={{ width: treeWidth }} className="h-full shrink-0 bg-side" aria-label={t('workspace.files.tree')}><FileTree onDirectories={onDirectories} refreshToken={treeRefresh} scope={scope} selected={file.data?.path ?? tab.ref} view={view} onView={onView} onSelect={(path) => void select(path)} /></aside>
        <SplitHandle side="left" value={treeWidth} min={140} max={maxTree} label={t('workspace.files.treeWidth')} onChange={(treeWidth) => onView({ treeWidth })} />
      </>}
      <div className="min-h-0 min-w-0 flex-1">
        {file.isError ? <EmptyState title={apiErrorMessage(t, file.error)} action={<Button size="sm" onClick={() => void file.refetch()}>{t('workspace.files.retry')}</Button>} /> : file.isPending ? <Spinner label={t('common.loading')} /> : file.data.kind === 'directory' ? <EmptyState icon={<Icons.view size={22} aria-hidden />} title={t('workspace.files.select')} /> : <FilePreview key={`${file.data.path}:${previewRefresh}`} scope={scope} file={file.data} view={view} onView={onView} />}
      </div>
    </div>
  </div>;
}

function FilePreview({ scope, file, view, onView }: { scope: WorkspaceScope; file: WorkspaceFile; view: View; onView: (view: View) => void }) {
  const { t } = useTranslation();
  const [source, setSource] = useState(!!view.line);
  useEffect(() => { if (view.line) setSource(true); }, [view.positionId]);
  if (file.size > 256 * 1024 * 1024) return <EmptyState title={t('workspace.files.tooLarge')} />;
  const viewer = pickViewer(file, source);
  return <div className="flex h-full min-h-0 flex-col">
    <div className="flex min-h-8 items-center justify-between border-b border-neutral-200 px-3 text-xs text-neutral-500 dark:border-neutral-800">
      <span>{t('workspace.files.readOnly')} · {new Intl.NumberFormat().format(file.size)} B</span>
      {showsSourceButton(file) && <Button size="sm" variant="ghost" onClick={() => setSource(!source)}>{t(source ? 'workspace.files.preview' : 'workspace.files.source')}</Button>}
    </div>
    <div className="min-h-0 flex-1"><Suspense fallback={<Spinner label={t('common.loading')} />}>{viewer ? <viewer.component key={viewer.perVersion ? file.version : undefined} scope={scope} file={file} view={view} onView={onView} /> : <EmptyState title={t('workspace.files.unsupported')} />}</Suspense></div>
  </div>;
}
