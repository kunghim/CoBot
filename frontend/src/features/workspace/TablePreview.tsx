import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { Button, EmptyState, Spinner } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { scopeKey } from './fileNavigation';
import { columnName, MAX_TABLE_BYTES, parseDelimited, type TableData } from './delimited';

export default function TablePreview({ scope, file }: { scope: WorkspaceScope; file: WorkspaceFile }) {
  const { t } = useTranslation();
  const data = useQuery({ queryKey: ['workspace-table', scopeKey(scope), file.path, file.version], queryFn: async ({ signal }) => {
    const bytes = await api.workspaceBytes(scope, file.path, signal, undefined, file.version);
    const text = new TextDecoder('utf-8', { fatal: true }).decode(await bytes.arrayBuffer());
    return parseDelimited(text, /\.tsv$/i.test(file.name) ? '\t' : ',');
  }, enabled: file.size <= MAX_TABLE_BYTES, retry: false, gcTime: 0 });
  if (file.size > MAX_TABLE_BYTES) return <EmptyState title={t('workspace.files.richTooLarge')} />;
  if (data.isError) return <EmptyState title={data.error.message === 'Table limit' ? t('workspace.files.tableLimit') : data.error.message === 'Invalid table' || data.error instanceof TypeError ? t('workspace.files.unreadableTable') : apiErrorMessage(t, data.error)} />;
  if (data.isPending) return <Spinner label={t('common.loading')} />;
  return <TableContents key={file.version} data={data.data} />;
}

export function TableContents({ data, literal = true }: { data: TableData; literal?: boolean }) {
  const { t } = useTranslation();
  const [rowPage, setRowPage] = useState(0);
  const [columnPage, setColumnPage] = useState(0);
  const rowStart = rowPage * 100;
  const columnStart = columnPage * 50;
  if (!data.rows.length) return <EmptyState title={t('workspace.files.emptyTable')} />;
  return <div className="flex h-full min-h-0 flex-col">
    <div className="flex flex-wrap items-center gap-2 border-b border-neutral-200 px-3 py-1 text-xs text-neutral-500 dark:border-neutral-800"><span>{t('workspace.files.tableSize', { rows: data.rows.length, columns: data.columns })}</span>{literal && <span>{t('workspace.files.tableLiteral')}</span>}</div>
    <div className="min-h-0 flex-1 overflow-auto"><table className="border-separate border-spacing-0 font-mono text-xs" aria-label={t('workspace.files.table')}>
      <thead className="sticky top-0 z-10 bg-side"><tr><th className="border-b border-r border-neutral-200 px-3 py-2 dark:border-neutral-800" aria-label={t('workspace.files.tableRow')} />{Array.from({ length: Math.min(50, data.columns - columnStart) }, (_, index) => <th key={index} scope="col" className="min-w-28 border-b border-r border-neutral-200 px-3 py-2 text-left dark:border-neutral-800">{columnName(columnStart + index)}</th>)}</tr></thead>
      <tbody>{data.rows.slice(rowStart, rowStart + 100).map((row, index) => <tr key={rowStart + index}><th scope="row" className="sticky left-0 border-b border-r border-neutral-200 bg-side px-3 py-2 font-normal text-neutral-500 dark:border-neutral-800">{rowStart + index + 1}</th>{Array.from({ length: Math.min(50, data.columns - columnStart) }, (_, column) => <td key={column} className="max-w-96 border-b border-r border-neutral-200 px-3 py-2 align-top dark:border-neutral-800"><div className="max-h-40 overflow-auto whitespace-pre-wrap break-words">{row[columnStart + column] ?? ''}</div></td>)}</tr>)}</tbody>
    </table></div>
    <div className="flex flex-wrap items-center justify-between gap-2 border-t border-neutral-200 px-3 py-1 text-xs dark:border-neutral-800">
      <div className="flex items-center gap-2"><Button size="sm" variant="ghost" disabled={!rowPage} onClick={() => setRowPage(rowPage - 1)}>{t('workspace.files.previousPage')}</Button><span>{t('workspace.files.tableRows', { start: rowStart + 1, end: Math.min(rowStart + 100, data.rows.length) })}</span><Button size="sm" variant="ghost" disabled={rowStart + 100 >= data.rows.length} onClick={() => setRowPage(rowPage + 1)}>{t('workspace.files.nextPage')}</Button></div>
      {data.columns > 50 && <div className="flex items-center gap-2"><Button size="sm" variant="ghost" disabled={!columnPage} onClick={() => setColumnPage(columnPage - 1)}>{t('workspace.files.previousColumns')}</Button><span>{columnName(columnStart)}–{columnName(Math.min(columnStart + 49, data.columns - 1))}</span><Button size="sm" variant="ghost" disabled={columnStart + 50 >= data.columns} onClick={() => setColumnPage(columnPage + 1)}>{t('workspace.files.nextColumns')}</Button></div>}
    </div>
  </div>;
}
