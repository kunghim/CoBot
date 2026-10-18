// Page structure: page headers, sections, panel headers, toolbars and the
// side panel toggles. Panels open and close from the same place with the
// same pair of icons, so the control never jumps.
import type { ReactNode } from 'react';
import { PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen } from 'lucide-react';
import { IconButton } from './Button';
import { cx } from './foundation';

/** The top of a page: its title, what it is for, and its main actions. */
export function PageHeader({ title, description, actions, eyebrow }: { title: ReactNode; description?: ReactNode; actions?: ReactNode; eyebrow?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end gap-4">
      <div className="min-w-0 flex-1">
        {eyebrow && <p className="mb-1 text-[11px] font-semibold uppercase tracking-[0.12em] text-neutral-500 dark:text-neutral-400">{eyebrow}</p>}
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
