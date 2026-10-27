import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { EmptyState, Select, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import { MAX_TABLE_BYTES } from './delimited';
import { TableContents } from './TablePreview';
import type { WorkbookData } from './workbook';

function loadWorkbook(buffer: ArrayBuffer, signal: AbortSignal): Promise<WorkbookData> {
  return new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const worker = new Worker(new URL('./workbook.worker.ts', import.meta.url), { type: 'module' });
    const finish = (error?: Error, data?: WorkbookData) => { clearTimeout(timer); signal.removeEventListener('abort', abort); worker.terminate(); if (error) reject(error); else resolve(data!); };
    const abort = () => finish(new DOMException('Aborted', 'AbortError'));
    const timer = setTimeout(() => finish(new Error('Workbook timeout')), 10000);
    signal.addEventListener('abort', abort, { once: true });
    worker.onerror = () => finish(new Error('Invalid workbook'));
    worker.onmessage = (event: MessageEvent<{ data?: WorkbookData; error?: string }>) => event.data.data ? finish(undefined, event.data.data) : finish(new Error(event.data.error === 'limit' ? 'Workbook limit' : 'Invalid workbook'));
    worker.postMessage(buffer, [buffer]);
  });
}

export default function WorkbookPreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const data = useQuery({ queryKey: ['workspace-workbook', scopeKey(scope), file.path, file.version], queryFn: async ({ signal }) => loadWorkbook(await (await api.workspaceBytes(scope, file.path, signal, undefined, file.version)).arrayBuffer(), signal), enabled: file.size <= MAX_TABLE_BYTES, retry: false, gcTime: 0 });
  if (file.size > MAX_TABLE_BYTES) return <EmptyState title={t('workspace.files.officeTooLarge')} />;
  if (data.isError) return <EmptyState title={data.error.message === 'Workbook limit' ? t('workspace.files.tableLimit') : data.error.message === 'Workbook timeout' ? t('workspace.files.workbookTimeout') : data.error.message === 'Invalid workbook' ? t('workspace.files.unreadableWorkbook') : apiErrorMessage(t, data.error)} />;
  if (data.isPending) return <Spinner label={t('common.loading')} />;
  return <WorkbookContents key={file.version} data={data.data} />;
}

export function WorkbookContents({ data }: { data: WorkbookData }) {
  const { t } = useTranslation();
  const [sheet, setSheet] = useState('0');
  const selected = data.sheets[Number(sheet)];
  if (!selected) return <EmptyState title={t('workspace.files.emptyTable')} />;
  return <div className="flex h-full min-h-0 flex-col">
    <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800"><Select size="sm" label={t('workspace.files.worksheet')} value={sheet} options={data.sheets.map((item, index) => ({ value: String(index), label: item.name }))} onChange={setSheet} /><span className="text-neutral-500">{t('workspace.files.workbookCached')}</span></div>
    <div className="min-h-0 flex-1"><TableContents key={sheet} data={selected.data} literal={false} /></div>
  </div>;
}
