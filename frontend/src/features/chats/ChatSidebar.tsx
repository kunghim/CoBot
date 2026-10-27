import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api } from '../../api/client';
import type { Chat, ChatNavigation, ChatNavigationInput, ChatSection } from '../../api/types';
import { Alert, AlertDialog, Button, IconButton, Icons, Menu, TextInput, cx, type MenuItem } from '../../components/ui';
import { readLocal, writeLocal } from '../../app/mode';
import { apiErrorMessage } from '../../i18n/errors';
import { useChatNavigation, useChats } from './queries';
import ChatMenu, { useRefreshChats } from './ChatMenu';

type SectionDialog = { action: 'create' | 'rename' | 'delete'; section?: ChatSection };
export default function ChatSidebar() {
  const { t } = useTranslation(); const navigate = useNavigate(); const navigation = useChatNavigation();
  const refresh = useRefreshChats(); const nav = navigation.data;
  const pins = useChats({ sort: nav?.sort ?? 'recent', pinned: 'only' });
  const hasPins = pins.data?.pages.some((page) => page.items.length > 0) ?? false;
  const [dialog, setDialog] = useState<SectionDialog | null>(null); const [name, setName] = useState('');
  const mutation = useMutation({ mutationFn: (input: Omit<ChatNavigationInput, 'expectedRevision'>) => api.changeChatNavigation({ ...input, expectedRevision: nav!.revision }), onSettled: refresh });
  const edit = (next: SectionDialog) => { mutation.reset(); setName(next.section?.name ?? ''); setDialog(next); };
  const prefs = (next: Partial<Pick<ChatNavigation, 'sort' | 'grouping'>>) => mutation.mutate({ action: 'preferences', sort: nav!.sort, grouping: nav!.grouping, ...next });
  const entries: Array<MenuItem | 'separator'> = [
    { id: 'grouping', label: t('chatNav.display'), icon: Icons.panelLeftClose, disabled: !nav || mutation.isPending, children: [
      { id: 'sections', label: t('chatNav.bySection'), checked: nav?.grouping === 'sections', onSelect: () => prefs({ grouping: 'sections' }) },
      { id: 'merged', label: t('chatNav.merged'), checked: nav?.grouping === 'merged', onSelect: () => prefs({ grouping: 'merged' }) },
    ] },
    { id: 'sort', label: t('chatNav.sort'), icon: Icons.filter, disabled: !nav || mutation.isPending, children: [
      { id: 'recent', label: t('chatNav.recent'), checked: nav?.sort === 'recent', onSelect: () => prefs({ sort: 'recent' }) },
      { id: 'manual', label: t('chatNav.manual'), checked: nav?.sort === 'manual', onSelect: () => prefs({ sort: 'manual' }) },
    ] },
    'separator', { id: 'create', label: t('chatNav.newSection'), icon: Icons.add, disabled: !nav || mutation.isPending, onSelect: () => edit({ action: 'create' }) },
    { id: 'archived', label: t('chatNav.archived'), icon: Icons.archive, onSelect: () => navigate('/settings/archived?type=chat') },
  ];
  return <div className="flex min-h-0 flex-1 flex-col">
    {/* The rail already offers new chat; the list title keeps a compact + so
        the action stays one click from the history it adds to. */}
    <div className="flex items-center justify-between px-4 pb-1 pt-2">
      <p className="text-xs font-medium text-neutral-500 dark:text-neutral-400">{t('chat.history')}</p>
      <div className="flex items-center gap-0.5">
        <IconButton size="sm" icon={Icons.add} label={t('chat.new')} onClick={() => navigate('/chat/new')} />
        <Menu label={t('chatNav.organize')} trigger={<IconButton size="sm" icon={Icons.more} label={t('chatNav.organize')} />} items={entries} />
      </div>
    </div>
    <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3">
      {(navigation.error || mutation.error) && !dialog && <Alert tone="danger">{apiErrorMessage(t, mutation.error ?? navigation.error)}</Alert>}
      {nav && <>
        <ChatBucket nav={nav} pinned />
        {nav.grouping === 'merged' ? <ChatBucket nav={nav} empty={!hasPins} /> : <>
          <ChatBucket nav={nav} section="_default" heading={nav.sections.length ? t('chatNav.unsectioned') : undefined} empty={!hasPins} />
          {nav.sections.map((section, i) => <ChatBucket key={section.id} nav={nav} section={section.id} heading={section.name} sectionMenu={<Menu label={t('chatNav.sectionMenu', { name: section.name })}
            trigger={<IconButton size="sm" icon={Icons.more} label={t('chatNav.sectionMenu', { name: section.name })} />} items={[
              { id: 'rename', label: t('chatNav.renameSection'), icon: Icons.edit, onSelect: () => edit({ action: 'rename', section }) },
              { id: 'up', label: t('chatNav.moveUp'), icon: Icons.collapse, disabled: i === 0 || mutation.isPending, onSelect: () => mutation.mutate({ action: 'reorder', id: section.id, beforeId: nav.sections[i - 1].id }) },
              { id: 'down', label: t('chatNav.moveDown'), icon: Icons.expand, disabled: i === nav.sections.length - 1 || mutation.isPending, onSelect: () => mutation.mutate({ action: 'reorder', id: section.id, beforeId: nav.sections[i + 2]?.id ?? '' }) },
              'separator', { id: 'delete', label: t('chatNav.deleteSection'), icon: Icons.delete, danger: true, onSelect: () => edit({ action: 'delete', section }) },
            ]} />} />)}
        </>}
      </>}
    </div>
    <AlertDialog open={!!dialog} title={t(dialog?.action === 'delete' ? 'chatNav.deleteSection' : dialog?.action === 'rename' ? 'chatNav.renameSection' : 'chatNav.newSection')}
      description={dialog?.action === 'delete' ? t('chatNav.deleteSectionNote') : undefined} danger={dialog?.action === 'delete'} busy={mutation.isPending}
      confirmLabel={t(dialog?.action === 'delete' ? 'common.delete' : 'common.save')} cancelLabel={t('common.cancel')}
      error={mutation.error ? apiErrorMessage(t, mutation.error) : undefined} confirmDisabled={dialog?.action !== 'delete' && !name.trim()}
      onCancel={() => { if (!mutation.isPending) setDialog(null); }} onConfirm={() => dialog && mutation.mutate({ action: dialog.action, id: dialog.section?.id, name }, { onSuccess: () => setDialog(null) })}>
      {dialog?.action !== 'delete' && <TextInput value={name} onChange={(e) => setName(e.target.value)} maxLength={60} aria-label={t('chatNav.sectionName')} />}
    </AlertDialog>
  </div>;
}

function ChatBucket({ nav, pinned = false, section, heading, sectionMenu, empty = false }: {
  nav: ChatNavigation; pinned?: boolean; section?: string; heading?: string; sectionMenu?: React.ReactNode; empty?: boolean;
}) {
  const { t } = useTranslation(); const { chatId } = useParams(); const navigate = useNavigate(); const refresh = useRefreshChats();
  const list = useChats({ sort: nav.sort, pinned: pinned ? 'only' : 'exclude', section });
  const items = [...new Map(list.data?.pages.flatMap((p) => p.items).map((c) => [c.id, c]) ?? []).values()];
  const collapsedKey = 'cobot.chat.section.' + (pinned ? 'pinned' : section ?? 'merged');
  const [closed, setClosed] = useState(() => readLocal(collapsedKey) === 'true');
  const reorder = useMutation({ mutationFn: ({ source, beforeId }: { source: Chat; beforeId: string }) => api.organizeChat(source.id, 'reorder', { expectedRevision: source.revision, beforeId }), onSettled: refresh });
  if (pinned && !items.length && !list.error) return null;
  const title = pinned ? t('chatNav.pinned') : heading;
  return <section aria-label={title || t('chat.history')} className="mb-2">
    {title && <div className="flex items-center gap-1 px-1">
      <button type="button" className="flex min-w-0 flex-1 items-center gap-1 rounded-lg px-1 py-2 text-xs font-medium text-neutral-500 hover:bg-neutral-900/5 dark:hover:bg-white/5" aria-expanded={!closed}
        onClick={() => { setClosed(!closed); writeLocal(collapsedKey, String(!closed)); }}>
        {closed ? <Icons.next size={12} aria-hidden /> : <Icons.expand size={12} aria-hidden />}<span className="truncate">{title}</span>
      </button>{sectionMenu}
    </div>}
    {(list.error || reorder.error) && <Alert tone="danger">{apiErrorMessage(t, reorder.error ?? list.error)}</Alert>}
    {!closed && <>
      {list.isPending && <p role="status" className="p-3 text-xs text-neutral-500">{t('common.loading')}</p>}
      <div className="space-y-0.5">{items.map((chat) => <div key={chat.id} data-chat-id={chat.id} draggable={nav.sort === 'manual' && !reorder.isPending} onDragStart={(e) => { e.dataTransfer.setData('application/x-cobot-chat', JSON.stringify(chat)); e.dataTransfer.effectAllowed = 'move'; }}
        onDragOver={(e) => { if (nav.sort === 'manual' && e.dataTransfer.types.includes('application/x-cobot-chat')) e.preventDefault(); }}
        onDrop={(e) => { e.preventDefault(); if (nav.sort !== 'manual') return; try { const source = JSON.parse(e.dataTransfer.getData('application/x-cobot-chat')) as Chat; if (source.id && source.id !== chat.id && !!source.pinned === !!chat.pinned && (nav.grouping === 'merged' || pinned || (source.sectionId ?? '') === (chat.sectionId ?? ''))) reorder.mutate({ source, beforeId: chat.id }); } catch { /* Ignore unrelated native drag data. */ } }}
        className={cx('group flex min-w-0 items-center rounded-lg hover:bg-neutral-900/5 dark:hover:bg-white/5', chat.id === chatId && 'bg-neutral-900/5 dark:bg-white/10')}>
        <button type="button" aria-current={chat.id === chatId ? 'page' : undefined} onClick={() => navigate(`/chat/${chat.id}`)} className="min-w-0 flex-1 truncate rounded-lg px-3 py-2 text-left text-sm">
          {chat.title}{chat.archiveOperation && <span className="ml-1 text-amber-600">· {t('chatNav.pendingShort')}</span>}
        </button>
        <span className="flex shrink-0 items-center pr-1 opacity-0 focus-within:opacity-100 group-hover:opacity-100"><ChatMenu chat={chat} /></span>
      </div>)}</div>
      {!items.length && !list.isPending && (empty || heading) && <p className="px-3 py-2 text-xs text-neutral-500">{t(heading ? 'chatNav.emptySection' : 'chat.emptyHistory')}</p>}
      {list.hasNextPage && <Button size="sm" variant="ghost" loading={list.isFetchingNextPage} onClick={() => void list.fetchNextPage()}>{t('chat.loadMore')}</Button>}
    </>}
  </section>;
}
