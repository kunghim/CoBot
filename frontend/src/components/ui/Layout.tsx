// Page structure: page headers, sections, panel headers, toolbars and the
// side panel toggles. Panels open and close from the same place with the
// same pair of icons, so the control never jumps.
import type { ReactNode } from 'react';
import { Copy, Minus, PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Square, X } from 'lucide-react';
import { IconButton } from './Button';
import { cx } from './foundation';

/** The top of a page: its title, what it is for, and its main actions. */
export function PageHeader({ title, description, actions, eyebrow }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; eyebrow?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end gap-4">
      <div className="min-w-0 flex-1">
        {eyebrow && <p className="mb-1 text-2xs font-semibold uppercase tracking-[0.12em] text-neutral-500 dark:text-neutral-400">{eyebrow}</p>}
        <h1 className="text-xl font-semibold tracking-tight text-neutral-900 dark:text-neutral-50">{title}</h1>
        {description && <p className="mt-1 text-sm leading-6 text-neutral-600 dark:text-neutral-300">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </header>
  );
}

/** A titled part of a page. */
export function Section({ title, description, children, className }: { title: ReactNode; description?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cx('space-y-4', className)}>
      <div>
        <h2 className="text-base font-semibold text-neutral-900 dark:text-neutral-50">{title}</h2>
        {description && <p className="mt-1 text-sm leading-6 text-neutral-500 dark:text-neutral-400">{description}</p>}
      </div>
      {children}
    </section>
  );
}

/** The header row of a panel or column: what it shows, and its controls. */
export function PanelHeader({ leading, title, subtitle, actions }: { leading?: ReactNode; title: ReactNode; subtitle?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="flex min-h-14 items-center gap-2.5 px-3 py-2">
      {leading}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-semibold leading-5 text-neutral-900 dark:text-neutral-50">{title}</p>
        {subtitle && <p className="truncate text-xs leading-4 text-neutral-500 dark:text-neutral-400">{subtitle}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-1">{actions}</div>}
    </div>
  );
}

/** A row of controls over content (filters, view options, actions). */
export function Toolbar({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="toolbar" aria-label={label} className="flex flex-wrap items-center gap-2">
      {children}
    </div>
  );
}

/**
 * Opens and closes a side panel. The same button, in the same place, shows
 * "open" or "close" for its side; it sits at that side's edge of the header.
 */
export function PanelToggle({ side, open, onToggle, openLabel, closeLabel }: { side: 'left' | 'right'; open: boolean; onToggle: () => void; openLabel: string; closeLabel: string }) {
  const icon = side === 'left' ? (open ? PanelLeftClose : PanelLeftOpen) : open ? PanelRightClose : PanelRightOpen;
  return <IconButton icon={icon} label={open ? closeLabel : openLabel} aria-expanded={open} onClick={onToggle} size="sm" />;
}

/** The window controls of a frameless desktop window on Windows. */
export interface WindowControls {
  minimizeLabel: string;
  maximizeLabel: string;
  restoreLabel: string;
  closeLabel: string;
  maximized: boolean;
  onMinimize: () => void;
  onToggleMaximize: () => void;
  onClose: () => void;
}

/**
 * The sidebar's top row: the search field and the mode capsule share it at
 * the secondary menu's right end. `inset` clears the macOS windowed corner
 * reservation so the row's controls never sit under the traffic lights.
 * On the mac desktop `slim` shrinks the row to the system titlebar's 28px
 * metrics, so the row's center is the traffic lights' center in every
 * window state and the controls never shift between fullscreen and
 * windowed. The row drags the window.
 */
export function SidebarHead({ leading, children, inset = false, slim = false }: { leading?: ReactNode; children?: ReactNode; inset?: boolean; slim?: boolean }) {
  const drag = { ['--wails-draggable' as string]: 'drag' };
  const noDrag = { ['--wails-draggable' as string]: 'no-drag' };
  return (
    <div data-testid="app-mode-header" style={drag} className={cx('flex shrink-0 items-center bg-side', slim ? 'h-7' : 'h-10', inset ? 'pl-8 pr-2' : 'px-2')}>
      <div className="flex h-full min-w-0 flex-1 items-center justify-end gap-2" style={noDrag} onDoubleClick={(event) => event.stopPropagation()}>
        {leading}
        {children}
      </div>
    </div>
  );
}

/**
 * The conversation column's own top row: the list toggle at its left edge
 * (flush with the sheet below), an optional title, page actions at the
 * right, and on Windows the window controls the bar used to carry. The row
 * drags the window; controls opt out.
 */
export function ConversationHead({ title, left, right, windowControls, inset = false, slim = false }: { title?: ReactNode; left?: ReactNode; right?: ReactNode; windowControls?: WindowControls; inset?: boolean; slim?: boolean }) {
  const noDrag = { ['--wails-draggable' as string]: 'no-drag' };
  const drag = { ['--wails-draggable' as string]: 'drag' };
  const control = 'flex h-full w-[clamp(1.75rem,8vw,2.75rem)] items-center justify-center text-neutral-500 transition-colors hover:bg-neutral-900/[0.07] hover:text-neutral-900 dark:text-neutral-400 dark:hover:bg-white/[0.08] dark:hover:text-neutral-100';
  return (
    <div
      data-testid="app-title-bar"
      style={drag}
      className={cx('flex shrink-0 select-none items-center bg-side', slim ? 'h-7' : 'h-10', inset && 'pl-8')}
      onDoubleClick={windowControls?.onToggleMaximize}
    >
      <div className="flex h-full items-center gap-1" style={noDrag} onDoubleClick={(event) => event.stopPropagation()}>
        {left}
      </div>
      <div className="flex h-full min-w-0 flex-1 items-center truncate px-2 text-xs font-medium text-neutral-600 dark:text-neutral-300">{title}</div>
      {right && <div className="flex h-full items-center gap-1 px-2" style={noDrag} onDoubleClick={(event) => event.stopPropagation()}>
        {right}
      </div>}
      {windowControls && (
        <div className="flex h-full items-stretch" style={noDrag} onDoubleClick={(event) => event.stopPropagation()}>
          <button type="button" aria-label={windowControls.minimizeLabel} title={windowControls.minimizeLabel} onClick={windowControls.onMinimize} className={control}>
            <Minus size={15} aria-hidden />
          </button>
          <button type="button" aria-label={windowControls.maximized ? windowControls.restoreLabel : windowControls.maximizeLabel} title={windowControls.maximized ? windowControls.restoreLabel : windowControls.maximizeLabel} onClick={windowControls.onToggleMaximize} className={control}>
            {windowControls.maximized ? <Copy size={13} aria-hidden /> : <Square size={12} aria-hidden />}
          </button>
          <button type="button" aria-label={windowControls.closeLabel} title={windowControls.closeLabel} onClick={windowControls.onClose} className={cx(control, 'hover:bg-red-600 hover:text-white dark:hover:bg-red-600 dark:hover:text-white')}>
            <X size={16} aria-hidden />
          </button>
        </div>
      )}
    </div>
  );
}
