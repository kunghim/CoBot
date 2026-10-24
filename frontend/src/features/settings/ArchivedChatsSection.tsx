import { useEffect, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { Chat } from '../../api/types';
import { Alert, AlertDialog, Button, Card, Checkbox, EmptyState, Icons, SearchInput, Select, Skeleton, cx, useToast } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { useChats } from '../chats/queries';
import { useRefreshChats } from '../chats/ChatMenu';

export default function ArchivedChatsSection({ onBusyChange }: { onBusyChange: (busy: boolean) => void }) {
  const { t, i18n } = useTranslation(); const navigate = useNavigate(); const refresh = useRefreshChats(); const toast = useToast(); const [params] = useSearchParams();
  const [search, setSearch] = useState(''); const [sort, setSort] = useState('archived'); const [selected, setSelected] = useState<ReadonlySet<string>>(() => new Set());
  const [deleting, setDeleting] = useState<Chat[] | null>(null); const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const focus = useRef<HTMLLIElement>(null);
  const list = useChats({ archived: true, sort, search });
  const items = [...new Map(list.data?.pages.flatMap((page) => page.items).map((chat) => [chat.id, chat]) ?? []).values()];
  const selectable = items.filter((chat) => !chat.archiveOperation && !chat.busy);
  const all = selectable.length > 0 && selectable.every((chat) => selected.has(chat.id));
  const some = !all && selectable.some((chat) => selected.has(chat.id));
  useEffect(() => { onBusyChange(busy); return () => onBusyChange(false); }, [busy, onBusyChange]);
  useEffect(() => { setSelected(new Set()); }, [search, sort]);
  useEffect(() => { focus.current?.scrollIntoView({ block: 'center' }); }, [params, list.data]);
  const date = (value: string) => new Date(value).toLocaleString(i18n.language);
  const toggle = (id: string) => setSelected((previous) => { const next = new Set(previous); if (next.has(id)) next.delete(id); else next.add(id); return next; });
  const restore = async (chat: Chat) => {
    setBusy(true); setError('');
    try { const result = await api.organizeChat(chat.id, 'restore', { expectedRevision: chat.revision });
      if (result.archiveOperation) setError(t('chatNav.archivePending')); else { setSelected((prev) => new Set([...prev].filter((id) => id !== chat.id))); toast.show({ tone: 'success', title: t('archivePage.chatRestored', { name: chat.title }) }); }
    } catch (err) { setError(apiErrorMessage(t, err)); } finally { refresh(); setBusy(false); }
  };
  const remove = async (snapshot: Chat[]) => {
    setBusy(true); setError(''); const failures: string[] = []; let lastError = '';
    for (const chat of snapshot) { try { await api.deleteChat(chat.id, chat.revision); } catch (err) { failures.push(chat.id); lastError = apiErrorMessage(t, err); } }
    setBusy(false); setDeleting(null); setSelected(new Set(failures)); refresh();
    if (failures.length) setError(t('archivePage.chatDeleteFailed', { count: failures.length, reason: lastError }));
    else toast.show({ tone: 'success', title: t('archivePage.chatsDeleted', { count: snapshot.length }) });
  };
  return <Card>
    <div className="flex flex-wrap items-center gap-2">
      <SearchInput className="min-w-0 flex-1" value={search} onChange={setSearch} placeholder={t('archivePage.chatSearch')} label={t('archivePage.chatSearch')} clearLabel={t('common.clearSearch')} />
      <div className="w-40"><Select label={t('archived.sort')} value={sort} onChange={setSort} options={[
        { value: 'archived', label: t('archived.sortArchivedAt') }, { value: 'created', label: t('archived.sortCreatedAt') }, { value: 'name', label: t('archived.sortName') },
      ]} /></div>
    </div>
    {items.length > 0 && <div className="mt-3 flex min-h-8 items-center gap-3 border-b border-neutral-200 pb-2 dark:border-neutral-800">
      <Checkbox checked={all} indeterminate={some} disabled={busy} onChange={() => setSelected((prev) => { const next = new Set(prev); for (const chat of selectable) { if (all) next.delete(chat.id); else next.add(chat.id); } return next; })} label={t('archived.selectAll')} />
      {selected.size > 0 && <div className="ml-auto"><Button size="sm" variant="danger" icon={Icons.delete} disabled={busy} onClick={() => setDeleting(selectable.filter((chat) => selected.has(chat.id)))}>{t('archived.deleteSelected', { count: selected.size })}</Button></div>}
    </div>}
    {(error || list.error) && <div className="mt-3"><Alert tone="danger" onClose={() => setError('')} closeLabel={t('common.close')}>{error || apiErrorMessage(t, list.error)}</Alert></div>}
    {list.isPending && <Skeleton className="mt-3 h-14 w-full" />}
    {list.isSuccess && !items.length && <EmptyState icon={<Icons.archive size={20} aria-hidden />} title={t(search ? 'archivePage.chatNoMatch' : 'archivePage.chatEmpty')} />}
    <ul className="mt-1 divide-y divide-neutral-200 dark:divide-neutral-800">
      {items.map((chat) => <li key={chat.id} ref={chat.id === params.get('chat') ? focus : undefined} className={cx('flex flex-wrap items-start gap-3 py-3', chat.id === params.get('chat') && '-mx-2 rounded-lg bg-signal-500/[0.06] px-2')}>
        <span className="pt-0.5"><Checkbox disabled={busy || !!chat.archiveOperation || chat.busy} checked={selected.has(chat.id)} onChange={() => toggle(chat.id)} label={<span className="sr-only">{t('archived.select', { name: chat.title })}</span>} /></span>
        <div className="min-w-0 flex-1"><button type="button" onClick={() => navigate(`/chat/${chat.id}`)} className="block max-w-full truncate text-left text-sm font-medium hover:underline">{chat.title}</button>
          <p className="truncate font-mono text-xs text-neutral-500 dark:text-neutral-400">{chat.model}</p>
          <p className="mt-0.5 text-xs text-neutral-500 dark:text-neutral-400">{t('archivePage.chatMeta', { created: date(chat.createdAt), archived: date(chat.archivedAt!) })}</p>
          {chat.archiveOperation && <p role="status" className="mt-1 text-xs text-amber-600 dark:text-amber-400">{t('chatNav.archivePending')}</p>}
        </div>
        <div className="flex shrink-0 gap-1.5"><Button size="sm" icon={Icons.restore} disabled={busy || chat.busy} onClick={() => void restore(chat)}>{t(chat.archiveOperation ? 'chatNav.verifyArchive' : 'chatNav.restore')}</Button>
          <Button size="sm" variant="danger" disabled={busy || !!chat.archiveOperation || chat.busy} onClick={() => setDeleting([chat])}>{t('chat.delete')}</Button></div>
      </li>)}
    </ul>
    {list.hasNextPage && <Button variant="ghost" size="sm" loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{t('chat.loadMore')}</Button>}
    <AlertDialog open={!!deleting} title={deleting?.length === 1 ? t('chat.delete') : t('archivePage.chatDeleteMany', { count: deleting?.length ?? 0 })} description={t('chat.deleteNote')} danger busy={busy}
      confirmLabel={t('chat.delete')} cancelLabel={t('common.cancel')} onCancel={() => { if (!busy) setDeleting(null); }} onConfirm={() => deleting && void remove(deleting)} />
  </Card>;
}
