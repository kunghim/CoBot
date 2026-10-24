// Things that float over the page. Menu: a short list of actions on
// something. Popover: a small panel of controls or detail. Tooltip: a few
// words naming a control, never the only place information lives. Dialog:
// a task that needs the page to wait. AlertDialog: confirm something that
// cannot be undone. Sheet: a side panel over the page for a task to finish
// first (never the team list or the work panel, which stay in the layout).
import { cloneElement, useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactElement, type ReactNode } from 'react';
import { Check, ChevronRight, X } from 'lucide-react';
import { createPortal } from 'react-dom';
import { useDialogFocus } from '../useDialogFocus';
import { Button } from './Button';
import { cx, layer, surface } from './foundation';
import { useDismiss } from './useDismiss';

import { placementClass, useFloatingPlacement, type Placement } from './useFloating';

export interface MenuItem {
  id: string;
  label: ReactNode;
  icon?: (props: { size?: number; 'aria-hidden'?: boolean }) => ReactNode;
  /** A keyboard shortcut shown on the right. */
  shortcut?: string;
  danger?: boolean;
  disabled?: boolean;
  /** A line under the label saying what the choice means. */
  description?: ReactNode;
  /** Set on every item of a one-of menu (a mode): marks the current one. */
  checked?: boolean;
  onSelect?: () => void;
  children?: Array<MenuItem | 'separator'>;
}

/** Menus render outside scroll containers so nested choices remain visible at sidebar edges. */
export function Menu({ trigger, items, placement = 'bottom-end', label, note }: { trigger: ReactElement; items: Array<MenuItem | 'separator'>; placement?: Placement; label: string; note?: ReactNode }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const anchor = useRef<HTMLElement | null>(null);
  const floating = useRef<HTMLDivElement>(null);
  const id = useId();
  const close = useCallback(() => { setOpen(false); anchor.current?.focus(); }, []);
  useEffect(() => {
    if (!open) return;
    const outside = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node) && !floating.current?.contains(event.target as Node)) close(); };
    const reposition = () => close();
    document.addEventListener('pointerdown', outside);
    window.addEventListener('resize', reposition);
    return () => { document.removeEventListener('pointerdown', outside); window.removeEventListener('resize', reposition); };
  }, [open, close]);
  return <div ref={root} className="relative inline-flex">
    {cloneElement(trigger, { ref: (node: HTMLElement | null) => { anchor.current = node; }, 'aria-haspopup': 'menu', 'aria-expanded': open, 'aria-controls': open ? id : undefined,
      onClick: () => setOpen(!open), onKeyDown: (event: KeyboardEvent) => { if (event.key === 'ArrowDown') { event.preventDefault(); setOpen(true); } },
    } as Record<string, unknown>)}
    {open && createPortal(<div ref={floating}><MenuPanel id={id} items={items} label={label} anchor={() => anchor.current} placement={placement} closeAll={close} note={note} /></div>, document.body)}
  </div>;
}

function MenuPanel({ id, items, label, anchor, placement = 'bottom-end', nested = false, closeAll, closeLevel, note }: {
  id: string; items: Array<MenuItem | 'separator'>; label: string; anchor: () => HTMLElement | null; placement?: Placement; nested?: boolean;
  closeAll: () => void; closeLevel?: () => void; note?: ReactNode;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const refs = useRef<Record<string, HTMLButtonElement | null>>({});
  const [child, setChild] = useState<string | null>(null);
  const [position, setPosition] = useState({ left: 0, top: 0, ready: false });
  const actions = items.filter((item): item is MenuItem => item !== 'separator');
  const enabled = actions.filter((item) => !item.disabled);
  const focus = (at: number) => { if (enabled.length) refs.current[enabled[(at + enabled.length) % enabled.length].id]?.focus(); };
  useLayoutEffect(() => {
    const button = anchor(); const el = panel.current;
    if (!button || !el) return;
    const a = button.getBoundingClientRect(); const b = el.getBoundingClientRect();
    let left = nested ? a.right + 4 : placement.endsWith('end') ? a.right - b.width : a.left;
    let top = nested ? a.top - 4 : placement.startsWith('top') ? a.top - b.height - 6 : a.bottom + 6;
    if (nested && left + b.width > window.innerWidth - 8) left = a.left - b.width - 4;
    if (!nested && top + b.height > window.innerHeight - 8 && a.top >= b.height + 8) top = a.top - b.height - 6;
    left = Math.max(8, Math.min(left, window.innerWidth - b.width - 8));
    top = Math.max(8, Math.min(top, window.innerHeight - b.height - 8));
    setPosition({ left, top, ready: true }); focus(0);
    // Anchor identity changes during parent renders; measure only when this panel mounts.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const current = actions.find((item) => item.id === child);
  return <>
    <div ref={panel} id={id} role="menu" aria-label={label} style={{ left: position.left, top: position.top, visibility: position.ready ? 'visible' : 'hidden' }}
      onKeyDown={(event) => {
        const at = enabled.findIndex((item) => refs.current[item.id] === document.activeElement);
        if (event.key === 'ArrowDown') focus(at + 1);
        else if (event.key === 'ArrowUp') focus(at - 1);
        else if (event.key === 'Home') focus(0);
        else if (event.key === 'End') focus(enabled.length - 1);
        else if (event.key === 'ArrowRight' && enabled[at]?.children?.length) setChild(enabled[at].id);
        else if (event.key === 'ArrowLeft' && closeLevel) closeLevel();
        else if (event.key === 'Escape') { if (closeLevel) closeLevel(); else closeAll(); }
        else if (event.key === 'Tab') { closeAll(); return; }
        else return;
        event.preventDefault(); event.stopPropagation();
      }} className={cx('fixed min-w-48 max-w-[calc(100vw-16px)] max-h-[calc(100vh-16px)] overflow-y-auto rounded-xl p-1', actions.some((item) => item.description) && 'w-72', surface.overlay, layer.dropdown)}>
      {items.map((item, index) => item === 'separator' ? <div key={`separator-${index}`} role="separator" className="my-1 h-px bg-neutral-200 dark:bg-neutral-700" /> :
        <button key={item.id} ref={(node) => { refs.current[item.id] = node; }} type="button" tabIndex={-1} role={item.checked === undefined ? 'menuitem' : 'menuitemradio'} aria-checked={item.checked}
          disabled={item.disabled} aria-haspopup={item.children ? 'menu' : undefined} aria-expanded={item.children ? child === item.id : undefined} aria-controls={item.children && child === item.id ? `${id}-${item.id}` : undefined}
          onPointerEnter={() => { setChild(item.disabled ? null : item.children ? item.id : null); }}
          onClick={() => { if (item.children) setChild(item.id); else { closeAll(); item.onSelect?.(); } }}
          className={cx('flex w-full gap-2.5 rounded-lg px-2.5 text-left text-sm outline-none transition-colors disabled:opacity-40', item.description ? 'items-start py-2' : 'items-center py-1.5',
            item.danger ? 'text-red-600 hover:bg-red-50 focus:bg-red-50 dark:text-red-400 dark:hover:bg-red-950 dark:focus:bg-red-950' : 'text-neutral-800 hover:bg-neutral-900/[0.06] focus:bg-neutral-900/[0.06] dark:text-neutral-100 dark:hover:bg-white/[0.08] dark:focus:bg-white/[0.08]')}>
          {item.icon && <span className={cx('shrink-0', !!item.description && 'mt-0.5')}><item.icon size={15} aria-hidden /></span>}
          <span className="min-w-0 flex-1"><span className={cx('block', item.description ? 'font-medium' : 'whitespace-nowrap')}>{item.label}</span>{item.description && <span className="mt-0.5 block text-xs text-neutral-500 dark:text-neutral-400">{item.description}</span>}</span>
          {item.shortcut && <kbd className="ml-4 whitespace-nowrap font-mono text-2xs text-neutral-400">{item.shortcut}</kbd>}
          {item.checked !== undefined && <Check size={14} aria-hidden className={cx('shrink-0 text-signal-500', !item.checked && 'opacity-0')} />}
          {item.children && <ChevronRight size={14} aria-hidden className="shrink-0 text-neutral-400" />}
        </button>)}
      {note && <p className="px-2.5 pb-1.5 pt-1 text-2xs text-neutral-500">{note}</p>}
    </div>
    {current?.children && <MenuPanel key={current.id} id={`${id}-${current.id}`} items={current.children} label={typeof current.label === 'string' ? current.label : label} anchor={() => refs.current[current.id]} nested closeAll={closeAll}
      closeLevel={() => { setChild(null); refs.current[current.id]?.focus(); }} />}
  </>;
}

/** A trigger and a small floating panel; Escape or a click outside closes it. */
export function Popover({ trigger, children, placement = 'bottom-start', label, className }: { trigger: ReactElement; children: ReactNode; placement?: Placement; label: string; className?: string }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const placed = useFloatingPlacement(panel, open, placement);
  const panelId = useId();
  useDismiss(root, open, () => setOpen(false));
  return (
    <div ref={root} className="relative inline-flex">
      {cloneElement(trigger, { 'aria-expanded': open, 'aria-controls': panelId, onClick: () => setOpen((value) => !value) } as Record<string, unknown>)}
      {open && (
        <div ref={panel} id={panelId} role="dialog" aria-label={label} className={cx('absolute w-72 animate-pop-in rounded-xl p-4 text-sm', surface.overlay, layer.dropdown, placementClass[placed], className)}>
          {children}
        </div>
      )}
    </div>
  );
}

/** A few words naming the control under the pointer or focus. */
export function Tooltip({ content, children, side = 'top' }: { content: ReactNode; children: ReactElement; side?: 'top' | 'bottom' }) {
  const [shown, setShown] = useState(false);
  const [shift, setShift] = useState(0);
  const tip = useRef<HTMLSpanElement>(null);
  const id = useId();
  const timer = useRef<number>(0);
  // Keep the tip inside the window near its edges.
  useLayoutEffect(() => {
    if (!shown || !tip.current) {
      setShift(0);
      return;
    }
    const rect = tip.current.getBoundingClientRect();
    if (rect.left < 8) setShift(8 - rect.left);
    else if (rect.right > window.innerWidth - 8) setShift(window.innerWidth - 8 - rect.right);
  }, [shown]);
  const show = () => {
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setShown(true), 400);
  };
  const hide = () => {
    window.clearTimeout(timer.current);
    setShown(false);
  };
  useEffect(() => () => window.clearTimeout(timer.current), []);
  return (
    <span className="relative inline-flex" onMouseEnter={show} onMouseLeave={hide} onFocus={() => setShown(true)} onBlur={hide} onKeyDown={(event) => event.key === 'Escape' && hide()}>
      {cloneElement(children, { 'aria-describedby': shown ? id : undefined } as Record<string, unknown>)}
      {shown && (
        <span ref={tip} id={id} role="tooltip" style={shift ? { marginLeft: shift } : undefined} className={cx('pointer-events-none absolute left-1/2 w-max max-w-60 -translate-x-1/2 animate-fade-in rounded-md bg-neutral-900 px-2 py-1 text-xs text-white shadow-lg dark:bg-neutral-100 dark:text-neutral-900', layer.tooltip, side === 'top' ? 'bottom-full mb-1.5' : 'top-full mt-1.5')}>
          {content}
        </span>
      )}
    </span>
  );
}

/** The backdrop and focus handling shared by Dialog, AlertDialog and Sheet. */
function Modal({ open, onClose, busy = false, children, position, labelledBy, describedBy, role = 'dialog', width = 'max-w-lg', maxHeight }: { open: boolean; onClose: () => void; busy?: boolean; children: ReactNode; position: 'center' | 'left' | 'right'; labelledBy: string; describedBy?: string; role?: 'dialog' | 'alertdialog'; width?: string; maxHeight?: string }) {
  const panel = useDialogFocus(open, onClose, busy);
  if (!open) return null;
  return (
    <div
      className={cx('fixed inset-0 flex animate-fade-in bg-neutral-950/45 backdrop-blur-[2px]', layer.dialog, position === 'center' && 'items-start justify-center p-4 pt-[10vh]', position === 'right' && 'justify-end', position === 'left' && 'justify-start')}
      onMouseDown={() => !busy && onClose()}
    >
      <div
        ref={panel as React.RefObject<HTMLDivElement>}
        tabIndex={-1}
        role={role}
        aria-modal="true"
        aria-labelledby={labelledBy}
        aria-describedby={describedBy}
        onMouseDown={(event) => event.stopPropagation()}
        // 100% is the room the overlay leaves, so a smaller window shrinks the
        // panel too; a caller's own limit can only lower it.
        style={position === 'center' && maxHeight ? { maxHeight: `min(${maxHeight}, 100%)` } : undefined}
        className={cx(
          'bg-card shadow-2xl outline-none',
          position === 'center' && cx('flex max-h-full w-full animate-pop-in flex-col rounded-2xl border border-neutral-200 dark:border-neutral-700', width),
          position === 'right' && 'flex h-full w-80 max-w-[90vw] animate-slide-in-right flex-col border-l border-neutral-200 dark:border-neutral-700',
          position === 'left' && 'flex h-full w-72 max-w-[90vw] animate-slide-in-left flex-col border-r border-neutral-200 dark:border-neutral-700',
        )}
      >
        {children}
      </div>
    </div>
  );
}

/** A task that needs the page to wait: a title, the content, and actions. */
export function Dialog({ open, onClose, title, description, children, footer, busy, closeLabel, size = 'md', maxHeight }: { open: boolean; onClose: () => void; title: ReactNode; description?: ReactNode; children?: ReactNode; footer?: ReactNode; busy?: boolean; closeLabel: string; size?: 'sm' | 'md' | 'lg'; maxHeight?: string }) {
  const id = useId();
  const width = { sm: 'max-w-sm', md: 'max-w-lg', lg: 'max-w-2xl' }[size];
  return (
    <Modal open={open} onClose={onClose} busy={busy} position="center" width={width} labelledBy={`${id}-title`} describedBy={description ? `${id}-description` : undefined} maxHeight={maxHeight}>
      <div className="flex min-h-0 flex-1 flex-col p-6">
        <div className="flex shrink-0 items-start gap-3">
          <div className="min-w-0 flex-1">
            <h2 id={`${id}-title`} className="text-lg font-semibold tracking-tight text-neutral-900 dark:text-neutral-50">{title}</h2>
            {description && <p id={`${id}-description`} className="mt-1.5 text-sm leading-6 text-neutral-600 dark:text-neutral-300">{description}</p>}
          </div>
          <button type="button" onClick={onClose} disabled={busy} aria-label={closeLabel} title={closeLabel} className="-mr-2 -mt-1 rounded-lg p-1.5 text-neutral-400 hover:bg-neutral-900/[0.06] hover:text-neutral-700 dark:hover:bg-white/[0.08] dark:hover:text-neutral-200">
            <X size={18} aria-hidden />
          </button>
        </div>
        {/* The title and the actions stay where they are; what lies between scrolls. */}
        {children && <div className="-mx-1 mt-5 min-h-0 flex-1 overflow-y-auto px-1">{children}</div>}
        {footer && <div className="mt-6 flex shrink-0 flex-wrap items-center justify-end gap-2">{footer}</div>}
      </div>
    </Modal>
  );
}

/**
 * Asks before an action with consequences; the confirm button names the
 * action. children hold a choice the action needs (which member to delete);
 * confirmDisabled keeps the action off until it is made.
 */
export function AlertDialog({ open, onCancel, onConfirm, title, description, confirmLabel, cancelLabel, danger = true, busy = false, error, children, confirmDisabled = false }: { open: boolean; onCancel: () => void; onConfirm: () => void; title: ReactNode; description?: ReactNode; confirmLabel: string; cancelLabel: string; danger?: boolean; busy?: boolean; error?: ReactNode; children?: ReactNode; confirmDisabled?: boolean }) {
  const id = useId();
  return (
    <Modal open={open} onClose={onCancel} busy={busy} position="center" width="max-w-sm" role="alertdialog" labelledBy={`${id}-title`} describedBy={description ? `${id}-description` : undefined}>
      <div className="p-6">
        <h2 id={`${id}-title`} className="text-base font-semibold text-neutral-900 dark:text-neutral-50">{title}</h2>
        {children && <div className="mt-4">{children}</div>}
        {description && <p id={`${id}-description`} className="mt-2 whitespace-pre-line text-sm leading-6 text-neutral-600 dark:text-neutral-300">{description}</p>}
        {error && <p className="mt-3 text-sm text-red-600 dark:text-red-400" role="alert">{error}</p>}
        <div className="mt-6 flex justify-end gap-2">
          <Button variant="ghost" onClick={onCancel} disabled={busy}>{cancelLabel}</Button>
          <Button variant={danger ? 'danger' : 'primary'} onClick={onConfirm} loading={busy} disabled={confirmDisabled}>{confirmLabel}</Button>
        </div>
      </div>
    </Modal>
  );
}

/**
 * A side panel over the page, for something to finish before going back.
 * Without a title the content brings its own header and close control;
 * label then names the panel.
 */
export function Sheet({ open, onClose, side = 'right', title, label, children, closeLabel }: { open: boolean; onClose: () => void; side?: 'left' | 'right'; title?: ReactNode; label?: string; children: ReactNode; closeLabel?: string }) {
  const id = useId();
  if (title === undefined) {
    return (
      <Modal open={open} onClose={onClose} position={side} labelledBy={`${id}-label`}>
        <span id={`${id}-label`} className="sr-only">{label}</span>
        <div className="flex min-h-0 flex-1 flex-col">{children}</div>
      </Modal>
    );
  }
  return (
    <Modal open={open} onClose={onClose} position={side} labelledBy={`${id}-title`}>
      <div className="flex items-center gap-2 border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
        <h2 id={`${id}-title`} className="min-w-0 flex-1 truncate text-sm font-semibold">{title}</h2>
        <button type="button" onClick={onClose} aria-label={closeLabel} title={closeLabel} className="rounded-lg p-1.5 text-neutral-500 hover:bg-neutral-900/[0.06] hover:text-neutral-900 dark:hover:bg-white/[0.08] dark:hover:text-neutral-100">
          <X size={16} aria-hidden />
        </button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto p-4">{children}</div>
    </Modal>
  );
}
