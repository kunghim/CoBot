// The header's conversation search: a small field that finds chats and team
// members by conversation content and jumps into the match. The API returns
// a few newest hits per group; archived and deleted conversations never
// appear there, so they never appear here.
import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation, useNavigate } from 'react-router-dom';
import { Icons, TextInput, cx, layer, surface } from '../../components/ui';
import { useDismiss } from '../../components/ui/useDismiss';
import { useConversationSearch } from '../../api/queries';
import { useSelectedBot } from '../../app/SelectedBotContext';
import { writeLocal } from '../../app/mode';

export default function SearchBox() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const location = useLocation();
  const { setBotId } = useSelectedBot();
  const listId = useId();
  const [value, setValue] = useState('');
  const [settled, setSettled] = useState('');
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  // A choose() closes the list itself; the route effect below must not also
  // close a list the user has already reopened by typing into the next query.
  const ownNavigation = useRef(false);

  // Keystrokes settle into one query; each settled phrase is one request.
  useEffect(() => {
    const timer = setTimeout(() => setSettled(value.trim()), 250);
    return () => clearTimeout(timer);
  }, [value]);
  useEffect(() => setActive(0), [settled]);
  // Arriving somewhere new closes the list, unless the jump was this
  // field's own pick (it closes itself, and the next keystroke reopens).
  useEffect(() => {
    if (ownNavigation.current) {
      ownNavigation.current = false;
      return;
    }
    setOpen(false);
  }, [location.pathname]);

  const search = useConversationSearch(settled);
  const chats = settled ? search.data?.chats ?? [] : [];
  const members = settled ? search.data?.members ?? [] : [];
  const total = chats.length + members.length;
  // Clearing the field closes at once instead of showing the stale list for
  // the rest of the debounce.
  const showList = open && settled !== '' && value.trim() !== '';
  useDismiss(root, showList, () => setOpen(false));
  // Keyboard movement follows the highlight into view.
  useEffect(() => {
    document.getElementById(`${listId}-${active}`)?.scrollIntoView({ block: 'nearest' });
  }, [active, listId, showList]);

  const choose = (index: number) => {
    setOpen(false);
    ownNavigation.current = true;
    if (index < chats.length) {
      navigate(`/chat/${chats[index].chatId}`);
      return;
    }
    const member = members[index - chats.length];
    // The team view is the work mode; leaving chat mode on record keeps the
    // home entry on the member instead of bouncing back to the chat.
    writeLocal('cobot.mode', 'work');
    setBotId(member.botId);
    navigate('/');
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    // Enter confirms an IME composition; it must not pick a result, and the
    // arrows must not move the highlight while a candidate list is open.
    if (event.nativeEvent.isComposing || event.keyCode === 229) return;
    if (!showList || total === 0) return;
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      setActive((index) => (index + 1) % total);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      setActive((index) => (index - 1 + total) % total);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      choose(active);
    }
  };

  const groupLabel = (key: string) => <p className="px-3 pb-1 pt-2 text-2xs font-semibold uppercase tracking-wider text-neutral-500 dark:text-neutral-400">{t(key)}</p>;
  const row = (key: string, title: string, snippet: string, index: number) => (
    <button
      key={key}
      id={`${listId}-${index}`}
      type="button"
      role="option"
      aria-selected={index === active}
      onMouseDown={(event) => event.preventDefault()}
      className={cx('block w-full rounded-lg px-3 py-1.5 text-left', index === active ? 'bg-neutral-100 dark:bg-neutral-700/60' : 'hover:bg-neutral-100 dark:hover:bg-neutral-700/60')}
      onMouseEnter={() => setActive(index)}
      onClick={() => choose(index)}
    >
      <span className="block truncate text-sm font-medium text-neutral-900 dark:text-neutral-100">{title}</span>
      {snippet !== '' && <span className="block truncate text-xs text-neutral-500 dark:text-neutral-400">{snippet}</span>}
    </button>
  );

  return (
    <div ref={root} className="relative min-w-0 max-w-36 flex-1">
      <TextInput
        size="xs"
        pill
        aria-label={t('searchBox.placeholder')}
        autoComplete="off"
        placeholder={t('searchBox.placeholder')}
        prefix={<Icons.search size={13} aria-hidden />}
        value={value}
        onChange={(event) => { setValue(event.target.value); setOpen(true); }}
        onKeyDown={onKeyDown}
        onFocus={() => settled !== '' && setOpen(true)}
        role="combobox"
        aria-expanded={showList}
        aria-controls={showList ? listId : undefined}
        aria-activedescendant={showList ? `${listId}-${active}` : undefined}
      />
      {showList && (
        <div id={listId} role="listbox" aria-label={t('searchBox.placeholder')} className={cx('absolute left-0 top-full mt-1 w-80 max-h-96 animate-pop-in overflow-y-auto rounded-xl p-1', surface.overlay, layer.dropdown)}>
          {total === 0 && <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{search.isError ? t('searchBox.error') : search.isFetching ? t('searchBox.searching') : t('searchBox.noResults')}</p>}
          {chats.length > 0 && groupLabel('searchBox.chats')}
          {chats.map((chat, index) => row(chat.chatId, chat.title, chat.snippet, index))}
          {members.length > 0 && groupLabel('searchBox.members')}
          {members.map((member, index) => row(member.botId, `${member.teamName} · ${member.botName}`, member.snippet, chats.length + index))}
        </div>
      )}
    </div>
  );
}
