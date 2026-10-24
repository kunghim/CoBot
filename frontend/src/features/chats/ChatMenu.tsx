import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { Chat } from '../../api/types';
import { AlertDialog, IconButton, Icons, Menu, TextInput, useToast, type MenuItem } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import { writeLocal } from '../../app/mode';
import { useChatNavigation } from './queries';

export function useRefreshChats() {
  const qc = useQueryClient();
  return () => { void qc.invalidateQueries({ queryKey: ['chats'] }); void qc.invalidateQueries({ queryKey: ['chat'] }); void qc.invalidateQueries({ queryKey: ['chat-navigation'] }); };
}

export default function ChatMenu({ chat, busy = false }: { chat: Chat; busy?: boolean }) {
  const { t } = useTranslation(); const navigate = useNavigate(); const { chatId } = useParams();
  const nav = useChatNavigation(); const refresh = useRefreshChats();
  const toast = useToast();
  const [dialog, setDialog] = useState<'rename' | 'delete' | null>(null);
  const [title, setTitle] = useState('');
  const [failure, setFailure] = useState('');
  const action = useMutation({ mutationFn: (fn: () => Promise<unknown>) => fn(), onSettled: refresh,
    onError: (error) => { const message = apiErrorMessage(t, error); setFailure(message); toast.show({ tone: 'danger', title: message }); },
  });
  const pending = !!chat.archiveOperation;
  const run = (fn: () => Promise<unknown>) => { setFailure(''); action.mutate(fn); };
  const lifecycle = (operation: 'archive' | 'restore') => run(async () => {
    const result = await api.organizeChat(chat.id, operation, { expectedRevision: chat.revision });
    if (result.archiveOperation) { setFailure(t('chatNav.archivePending')); toast.show({ tone: 'warning', title: t('chatNav.archivePending') }); return; }
    if (operation === 'archive' && chatId === chat.id) { writeLocal('cobot.chat.selected', ''); navigate('/chat/new'); }
  });
  const entries: Array<MenuItem | 'separator'> = [];
  if (pending) entries.push({ id: 'verifyArchive', label: t('chatNav.verifyArchive'), icon: Icons.refresh, disabled: action.isPending, onSelect: () => lifecycle(chat.archiveOperation!) });
  if (!chat.archivedAt && !pending) entries.push(
    { id: 'pin', label: t(chat.pinned ? 'chatNav.unpin' : 'chatNav.pin'), icon: chat.pinned ? Icons.unpin : Icons.pin, disabled: action.isPending, onSelect: () => run(() => api.organizeChat(chat.id, chat.pinned ? 'unpin' : 'pin', { expectedRevision: chat.revision })) },
    { id: 'move', label: t('chatNav.move'), icon: Icons.openFolder, disabled: action.isPending || !nav.data, children: [
      { id: 'default', label: t('chatNav.unsectioned'), checked: !chat.sectionId, onSelect: () => run(() => api.organizeChat(chat.id, 'move', { expectedRevision: chat.revision, sectionId: '' })) },
      ...(nav.data?.sections ?? []).map((s) => ({ id: s.id, label: s.name, checked: s.id === chat.sectionId, onSelect: () => run(() => api.organizeChat(chat.id, 'move', { expectedRevision: chat.revision, sectionId: s.id })) })),
    ] },
  );
  if (!chat.archivedAt && !pending && nav.data?.sort === 'manual') entries.push(
    { id: 'raise', label: t('chatNav.moveUp'), icon: Icons.collapse, disabled: action.isPending, onSelect: () => run(() => api.organizeChat(chat.id, 'reorder', { expectedRevision: chat.revision, beforeId: ':up' })) },
    { id: 'lower', label: t('chatNav.moveDown'), icon: Icons.expand, disabled: action.isPending, onSelect: () => run(() => api.organizeChat(chat.id, 'reorder', { expectedRevision: chat.revision, beforeId: ':down' })) },
  );
  entries.push(
    { id: 'rename', label: t('chat.rename'), icon: Icons.edit, disabled: pending || action.isPending, onSelect: () => { setTitle(chat.title); setFailure(''); setDialog('rename'); } },
    { id: 'folder', label: t('chatNav.openFolder'), icon: Icons.openFolder, disabled: action.isPending, onSelect: () => run(() => api.openChatPath(chat.id, '.', false)) },
  );
  if (!pending) entries.push({ id: 'archive', label: t(chat.archivedAt ? 'chatNav.restore' : 'chatNav.archive'), icon: chat.archivedAt ? Icons.restore : Icons.archive, disabled: busy || chat.busy || action.isPending, onSelect: () => lifecycle(chat.archivedAt ? 'restore' : 'archive') });
  entries.push('separator', { id: 'delete', label: t('chat.delete'), icon: Icons.delete, danger: true, disabled: busy || chat.busy || pending || action.isPending, onSelect: () => { setFailure(''); setDialog('delete'); } });
  return <>
    <Menu label={t('chat.menu')} trigger={<IconButton size="sm" icon={Icons.more} label={t('chat.menu')} disabled={action.isPending} />} items={entries} />
    {failure && !dialog && <span role="alert" className="sr-only">{failure}</span>}
    {failure && !dialog && <button type="button" aria-label={failure} title={failure} onClick={() => setFailure('')} className="text-red-600"><Icons.warning size={14} aria-hidden /></button>}
    <AlertDialog open={!!dialog} onCancel={() => { if (!action.isPending) setDialog(null); }} title={t(dialog === 'delete' ? 'chat.delete' : 'chat.rename')} description={dialog === 'delete' ? t('chat.deleteNote') : undefined}
      confirmLabel={t(dialog === 'delete' ? 'chat.delete' : 'common.save')} cancelLabel={t('common.cancel')} busy={action.isPending} danger={dialog === 'delete'} error={failure || undefined} confirmDisabled={dialog === 'rename' && !title.trim()}
      onConfirm={() => run(async () => {
        if (dialog === 'rename') await api.renameChat(chat.id, title, chat.revision);
        else { await api.deleteChat(chat.id, chat.revision); if (chatId === chat.id) { writeLocal('cobot.chat.selected', ''); navigate('/chat/new'); } }
        setDialog(null);
      })}>
      {dialog === 'rename' && <TextInput value={title} onChange={(e) => setTitle(e.target.value)} maxLength={120} aria-label={t('chat.title')} />}
    </AlertDialog>
  </>;
}
