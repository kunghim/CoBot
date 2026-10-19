// Navigation: the icon rail, side navigation items, tabs, breadcrumbs, steps
// and paging. Every item has three visible states — rest, hover (a layer of
// ink) and current (a filled row with a signal bar) — and a focus ring.
import { useRef, type KeyboardEvent, type ReactNode } from 'react';
import { ChevronRight } from 'lucide-react';
import { Check } from 'lucide-react';
import { cx, interactive } from './foundation';

type IconType = (props: { size?: number; 'aria-hidden'?: boolean }) => ReactNode;

/** The rail's icon entries (Home, Settings). Current shows a filled tile. */
export function RailItem({ icon: Icon, label, current = false, onClick, href, badge }: { icon: IconType; label: string; current?: boolean; onClick?: () => void; href?: string; badge?: ReactNode }) {
  const className = cx(
    'relative flex h-9 w-9 items-center justify-center rounded-lg transition-colors',
    current ? interactive.selected : `text-neutral-500 hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100 ${interactive.hover}`,
  );
  const content = (
    <>
      <Icon size={18} aria-hidden />
      {badge && <span className="absolute -right-0.5 -top-0.5">{badge}</span>}
    </>
  );
  return href ? (
    <a href={href} aria-label={label} title={label} aria-current={current ? 'page' : undefined} className={className}>
      {content}
    </a>
  ) : (
    <button type="button" onClick={onClick} aria-label={label} title={label} aria-current={current ? 'page' : undefined} className={className}>
      {content}
    </button>
  );
}

/**
 * A row of a side navigation or list that leads somewhere. With href it is a
 * link (it can open in a new tab); onClick then handles a plain click, so a
 * router can navigate without reloading.
 */
export function NavItem({ icon: Icon, label, current = false, onClick, href, trailing, leading, description, className }: { icon?: IconType; label: ReactNode; current?: boolean; onClick?: () => void; href?: string; trailing?: ReactNode; leading?: ReactNode; description?: ReactNode; className?: string }) {
  const classes = cx(
    'relative flex w-full min-w-0 items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-sm transition-colors',
    current ? cx(interactive.selected, 'font-medium') : cx('text-neutral-600 hover:text-neutral-900 dark:text-neutral-300 dark:hover:text-neutral-50', interactive.hover),
    className,
  );
  const content = (
    <>
      {current && <span aria-hidden className="absolute inset-y-1.5 left-0 w-0.5 rounded-full bg-signal-500" />}
      {leading ?? (Icon && <Icon size={16} aria-hidden />)}
      <span className="min-w-0 flex-1">
        <span className="block truncate">{label}</span>
        {description && <span className="block truncate text-xs font-normal text-neutral-500 dark:text-neutral-400">{description}</span>}
      </span>
      {trailing}
    </>
  );
  if (href) {
    return (
      <a
        href={href}
        aria-current={current ? 'page' : undefined}
        className={classes}
        onClick={(event) => {
          if (!onClick || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
          event.preventDefault();
          onClick();
        }}
      >
        {content}
      </a>
    );
  }
  return (
    <button type="button" onClick={onClick} aria-current={current ? 'page' : undefined} className={classes}>
      {content}
    </button>
  );
}

/** A small uppercase heading over a group of navigation items. */
export function NavSection({ title, children, action }: { title: ReactNode; children: ReactNode; action?: ReactNode }) {
  return (
    <div className="space-y-0.5">
      <div className="flex items-center justify-between px-2.5 pb-1">
        <span className="text-2xs font-semibold uppercase tracking-[0.12em] text-neutral-500 dark:text-neutral-400">{title}</span>
        {action}
      </div>
      {children}
    </div>
  );
}

export interface TabItem<T extends string> {
  id: T;
  label: ReactNode;
  count?: number;
}

/** Tabs switch views of the same thing. Arrow keys move between tabs. */
export function Tabs<T extends string>({ label, tabs, value, onChange, variant = 'underline', idPrefix }: { label: string; tabs: TabItem<T>[]; value: T; onChange: (value: T) => void; variant?: 'underline' | 'pill'; idPrefix: string }) {
  const refs = useRef<Array<HTMLButtonElement | null>>([]);
  const onKeyDown = (event: KeyboardEvent) => {
    const at = tabs.findIndex((tab) => tab.id === value);
    let next: number;
    if (event.key === 'ArrowRight') next = (at + 1) % tabs.length;
    else if (event.key === 'ArrowLeft') next = (at - 1 + tabs.length) % tabs.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = tabs.length - 1;
    else return;
    event.preventDefault();
    onChange(tabs[next].id);
    refs.current[next]?.focus();
  };
  return (
    <div role="tablist" aria-label={label} onKeyDown={onKeyDown} className={cx('flex', variant === 'underline' ? 'gap-5 border-b border-neutral-200 dark:border-neutral-800' : 'gap-1')}>
      {tabs.map((tab, index) => {
        const selected = tab.id === value;
        return (
          <button
            key={tab.id}
            ref={(element) => {
              refs.current[index] = element;
            }}
            id={`${idPrefix}-tab-${tab.id}`}
            type="button"
            role="tab"
            aria-selected={selected}
            aria-controls={`${idPrefix}-panel`}
            tabIndex={selected ? 0 : -1}
            onClick={() => onChange(tab.id)}
            className={cx(
              'inline-flex items-center gap-1.5 text-sm transition-colors',
              variant === 'underline' && cx('-mb-px border-b-2 py-2', selected ? 'border-neutral-900 font-semibold text-neutral-900 dark:border-neutral-100 dark:text-neutral-50' : 'border-transparent text-neutral-500 hover:border-neutral-300 hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100'),
              variant === 'pill' && cx('rounded-lg px-3 py-1.5', selected ? cx(interactive.selected, 'font-medium') : cx('text-neutral-500 hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100', interactive.hover)),
            )}
          >
            {tab.label}
            {tab.count !== undefined && <span className="rounded-full bg-neutral-900/[0.07] px-1.5 text-2xs tabular-nums dark:bg-white/[0.1]">{tab.count}</span>}
          </button>
        );
      })}
    </div>
  );
}

/** The panel a Tabs value shows. */
export function TabPanel({ idPrefix, value, children, className }: { idPrefix: string; value: string; children: ReactNode; className?: string }) {
  return (
    <div id={`${idPrefix}-panel`} role="tabpanel" aria-labelledby={`${idPrefix}-tab-${value}`} className={className}>
      {children}
    </div>
  );
}

/** Where a page sits; the last crumb is the current page and not a link. */
export function Breadcrumbs({ label, items }: { label: string; items: Array<{ label: ReactNode; onClick?: () => void }> }) {
  return (
    <nav aria-label={label}>
      <ol className="flex flex-wrap items-center gap-1 text-sm">
        {items.map((item, index) => {
          const last = index === items.length - 1;
          return (
            <li key={index} className="flex items-center gap-1">
              {last ? (
                <span aria-current="page" className="font-medium text-neutral-900 dark:text-neutral-50">{item.label}</span>
              ) : (
                <button type="button" onClick={item.onClick} className="rounded text-neutral-500 hover:text-neutral-900 hover:underline dark:text-neutral-400 dark:hover:text-neutral-100">{item.label}</button>
              )}
              {!last && <ChevronRight size={14} aria-hidden className="text-neutral-400" />}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

/** Steps of a short flow (create a team: folder → members → done). */
export function Steps({ label, steps, current }: { label: string; steps: ReactNode[]; current: number }) {
  return (
    <ol aria-label={label} className="flex flex-wrap items-center gap-2 text-sm">
      {steps.map((step, index) => {
        const done = index < current;
        const active = index === current;
        return (
          <li key={index} aria-current={active ? 'step' : undefined} className="flex items-center gap-2">
            <span className={cx('flex h-6 w-6 items-center justify-center rounded-full text-xs font-semibold', done && 'bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900', active && 'bg-signal-600 text-on-signal dark:bg-signal-500', !done && !active && 'bg-neutral-900/[0.07] text-neutral-500 dark:bg-white/[0.1]')}>
              {done ? <Check size={13} strokeWidth={3} aria-hidden /> : index + 1}
            </span>
            <span className={cx(active ? 'font-medium text-neutral-900 dark:text-neutral-50' : 'text-neutral-500 dark:text-neutral-400')}>{step}</span>
            {index < steps.length - 1 && <span aria-hidden className="mx-1 h-px w-6 bg-neutral-300 dark:bg-neutral-700" />}
          </li>
        );
      })}
    </ol>
  );
}

/** Pages of a long list: previous, the page numbers around the current one, next. */
export function Pagination({ label, page, pages, onChange, previousLabel, nextLabel }: { label: string; page: number; pages: number; onChange: (page: number) => void; previousLabel: string; nextLabel: string }) {
  const around = Array.from({ length: pages }, (_, i) => i + 1).filter((p) => p === 1 || p === pages || Math.abs(p - page) <= 1);
  const button = 'inline-flex h-8 min-w-8 items-center justify-center rounded-lg px-2 text-sm tabular-nums transition-colors disabled:opacity-40';
  return (
    <nav aria-label={label} className="flex items-center gap-1">
      <button type="button" disabled={page <= 1} onClick={() => onChange(page - 1)} className={cx(button, interactive.hover)}>{previousLabel}</button>
      {around.map((p, index) => (
        <span key={p} className="flex items-center gap-1">
          {index > 0 && p - around[index - 1] > 1 && <span className="px-1 text-neutral-400">…</span>}
          <button type="button" aria-current={p === page ? 'page' : undefined} onClick={() => onChange(p)} className={cx(button, p === page ? cx(interactive.selected, 'font-semibold') : interactive.hover)}>{p}</button>
        </span>
      ))}
      <button type="button" disabled={page >= pages} onClick={() => onChange(page + 1)} className={cx(button, interactive.hover)}>{nextLabel}</button>
    </nav>
  );
}
