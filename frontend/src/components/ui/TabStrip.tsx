// A closable, scrollable tab strip following the ARIA tablist pattern: the
// arrow keys move between tabs, Home/End jump to the ends, Delete closes the
// focused one. It carries its own new-tab and collapse actions so the strip
// is the one place tabs are managed.
import { useRef, type KeyboardEvent, type ReactNode } from 'react';
import { Icons } from './icons';
import { IconButton } from './Button';
import { cx, interactive } from './foundation';

export interface TabStripItem {
  id: string;
  label: string;
  icon?: ReactNode;
  /** A tab whose page is gone says so instead of showing a stale title. */
  muted?: boolean;
}

export interface TabStripProps {
  tabs: TabStripItem[];
  activeId: string | null;
  label: string;
  closeTabLabel: string;
  onSelect: (id: string) => void;
  onClose: (id: string) => void;
  onNew?: () => void;
  newTabLabel?: string;
  onCollapse?: () => void;
  collapseLabel?: string;
}

export function TabStrip({
  tabs,
  activeId,
  label,
  closeTabLabel,
  onSelect,
  onClose,
  onNew,
  newTabLabel,
  onCollapse,
  collapseLabel,
}: TabStripProps) {
  const listRef = useRef<HTMLDivElement>(null);

  const focusTab = (id: string) => {
    onSelect(id);
    const element = listRef.current?.querySelector<HTMLButtonElement>(`[data-tab-id="${CSS.escape(id)}"]`);
    element?.focus();
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const index = tabs.findIndex((tab) => tab.id === activeId);
    if (index < 0) return;
    let next = -1;
    if (event.key === 'ArrowRight') next = (index + 1) % tabs.length;
    else if (event.key === 'ArrowLeft') next = (index - 1 + tabs.length) % tabs.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = tabs.length - 1;
    else if (event.key === 'Delete') {
      event.preventDefault();
      onClose(tabs[index].id);
      return;
    } else return;
    event.preventDefault();
    focusTab(tabs[next].id);
  };

  return (
    <div className="flex min-h-10 items-center gap-1 pr-1">
      <div ref={listRef} role="tablist" aria-label={label} onKeyDown={onKeyDown} className="flex min-w-0 flex-1 items-end gap-1 overflow-x-auto pb-px">
        {tabs.map((tab) => {
          const active = tab.id === activeId;
          return (
            <div
              key={tab.id}
              className={cx(
                'group flex shrink-0 items-center rounded-t-lg',
                active ? 'bg-canvas' : `text-neutral-500 hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100 ${interactive.hover}`,
              )}
            >
              <button
                type="button"
                role="tab"
                data-tab-id={tab.id}
                id={`tab-${tab.id}`}
                aria-selected={active}
                aria-controls={`tabpanel-${tab.id}`}
                tabIndex={active ? 0 : -1}
                onClick={() => onSelect(tab.id)}
                className={cx(
                  'flex min-w-0 max-w-44 items-center gap-1.5 rounded-t-lg py-1.5 pl-2.5 pr-1 text-xs font-medium',
                  active ? 'text-neutral-900 dark:text-neutral-50' : '',
                )}
              >
                {tab.icon && <span className="shrink-0 opacity-70">{tab.icon}</span>}
                <span className={cx('truncate', tab.muted && 'italic opacity-70')}>{tab.label}</span>
              </button>
              <IconButton
                size="sm"
                icon={Icons.close}
                label={`${closeTabLabel} ${tab.label}`}
                className="h-6 w-6 opacity-60 group-hover:opacity-100"
                onClick={() => onClose(tab.id)}
              />
            </div>
          );
        })}
      </div>
      {onNew && newTabLabel && <IconButton size="sm" icon={Icons.add} label={newTabLabel} onClick={onNew} />}
      {onCollapse && collapseLabel && <IconButton size="sm" icon={Icons.panelRightClose} label={collapseLabel} onClick={onCollapse} />}
    </div>
  );
}
