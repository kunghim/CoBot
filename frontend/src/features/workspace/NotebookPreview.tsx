import { useMemo, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { Button, EmptyState, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import Markdown from '../conversations/Markdown';
import { scopeKey } from './fileNavigation';
import { inertHTML, MAX_NOTEBOOK_BYTES, parseNotebook, type NotebookCell } from './notebook';

export default function NotebookPreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const notebook = useQuery({
    queryKey: ['workspace-notebook', scopeKey(scope), file.path, file.version],
    queryFn: async ({ signal }) => parseNotebook(await (await api.workspaceBytes(scope, file.path, signal, undefined, file.version)).text()),
    enabled: file.size <= MAX_NOTEBOOK_BYTES,
    retry: false,
    gcTime: 0,
  });
  if (file.size > MAX_NOTEBOOK_BYTES) return <EmptyState title={t('workspace.files.richTooLarge')} />;
  if (notebook.isError) return <EmptyState title={notebook.error instanceof SyntaxError || notebook.error.message.startsWith('Invalid') || notebook.error.message.startsWith('Notebook') ? t('workspace.files.unreadableNotebook') : apiErrorMessage(t, notebook.error)} />;
  if (notebook.isPending) return <Spinner label={t('common.loading')} />;
  return <NotebookContents cells={notebook.data} scope={scope} path={file.path} />;
}

export function NotebookContents({ cells, scope, path }: { cells: NotebookCell[]; scope: WorkspaceScope; path: string }) {
  const { t } = useTranslation();
  const [page, setPage] = useState(0);
  const current = Math.min(page, Math.max(0, Math.ceil(cells.length / 50) - 1));
  const rendered = useMemo(() => cells.slice(current * 50, (current + 1) * 50).map((cell) => ({ ...cell, outputs: cell.outputs.map((output) => output.kind === 'html' ? { ...output, value: inertHTML(output.value) } : output) })), [cells, current]);
  const markdown = (value: string, imageSources?: Record<string, string>) => <Markdown text={value} teamId={scope.kind === 'team' ? scope.id : undefined} chatId={scope.kind === 'chat' ? scope.id : undefined} basePath={path} imageSources={imageSources} />;
  return <div className="flex h-full min-h-0 flex-col">
    <div className="min-h-0 flex-1 space-y-4 overflow-auto p-4 text-sm">
      {!cells.length && <EmptyState title={t('workspace.files.emptyNotebook')} />}
      {rendered.map((cell, index) => <section key={current * 50 + index} className="rounded-lg border border-neutral-200 p-3 dark:border-neutral-800" aria-label={t('workspace.files.notebookCell', { index: current * 50 + index + 1 })}>
        {cell.kind === 'code' && <div className="mb-2 font-mono text-xs text-neutral-500">In [{cell.execution ?? ' '}]:</div>}
        {cell.kind === 'markdown' ? markdown(cell.source, cell.attachments) : <pre className="overflow-auto whitespace-pre-wrap font-mono text-xs">{cell.source}</pre>}
        {cell.outputs.map((output, outputIndex) => <div key={outputIndex} className="mt-3 overflow-auto border-t border-neutral-200 pt-3 dark:border-neutral-800">
          {output.kind === 'image' ? <img src={output.value} alt={t('workspace.files.notebookOutput', { index: outputIndex + 1 })} className="max-w-full" /> : output.kind === 'html' ? <iframe title={t('workspace.files.notebookOutput', { index: outputIndex + 1 })} style={{ colorScheme: 'light' }} sandbox="" srcDoc={output.value} className="h-64 w-full bg-white" /> : output.kind === 'markdown' ? markdown(output.value) : <pre className="whitespace-pre-wrap font-mono text-xs">{output.value}</pre>}
          {output.truncated && <p className="mt-2 text-xs text-neutral-500">{t('workspace.files.notebookTruncated')}</p>}
        </div>)}
        {cell.truncated && <p className="mt-2 text-xs text-neutral-500">{t('workspace.files.notebookTruncated')}</p>}
      </section>)}
    </div>
    {cells.length > 50 && <div className="flex items-center justify-between border-t border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800">
      <Button size="sm" variant="ghost" disabled={!current} onClick={() => setPage(current - 1)}>{t('workspace.files.previousPage')}</Button>
      <span>{current + 1} / {Math.ceil(cells.length / 50)}</span>
      <Button size="sm" variant="ghost" disabled={(current + 1) * 50 >= cells.length} onClick={() => setPage(current + 1)}>{t('workspace.files.nextPage')}</Button>
    </div>}
  </div>;
}
