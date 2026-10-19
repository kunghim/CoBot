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
    <div className="flex min-h-14 items-center gap-2.5 border-b border-neutral-200 px-3 py-2 dark:border-neutral-800">
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
 * The app's own title bar in the desktop shell, where the system bar is
 * hidden: the panel toggles sit at its two ends, the middle drags the
 * window, and it follows CoBot's theme instead of the system's. On macOS
 * the system traffic lights stay on the left; on Windows the bar draws
 * minimize, maximize and close itself. The web app has no title bar; the
 * toggles stay in the conversation header there.
 */
export function AppTitleBar({ platform, title, left, right, windowControls }: { platform: 'windows' | 'mac'; title: ReactNode; left?: ReactNode; right?: ReactNode; windowControls?: WindowControls }) {
  const noDrag = { ['--wails-draggable' as string]: 'no-drag' };
  const control = 'flex h-full w-11 items-center justify-center text-neutral-500 transition-colors hover:bg-neutral-900/[0.07] hover:text-neutral-900 dark:text-neutral-400 dark:hover:bg-white/[0.08] dark:hover:text-neutral-100';
  return (
    <div
      style={{ ['--wails-draggable' as string]: 'drag' }}
      className="flex h-10 shrink-0 select-none items-center border-b border-neutral-200 bg-neutral-100 dark:border-neutral-800 dark:bg-neutral-900"
      onDoubleClick={windowControls?.onToggleMaximize}
    >
      {platform === 'mac' && <span className="w-[76px] shrink-0" aria-hidden />}
      <div className="flex items-center gap-1 px-2" style={noDrag}>
        {left}
      </div>
      <div className="min-w-0 flex-1 truncate text-center text-xs font-medium text-neutral-600 dark:text-neutral-300">{title}</div>
      <div className="flex items-center gap-1 px-2" style={noDrag}>
        {right}
      </div>
      {platform === 'windows' && windowControls && (
        <div className="flex h-full items-stretch" style={noDrag}>
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