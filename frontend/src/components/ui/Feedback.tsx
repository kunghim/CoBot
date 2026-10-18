// Feedback that stays in place: alerts (something the reader should know
// here), progress (spinner, bar, skeleton), empty states (what to do next)
// and small markers (badge, tag, status dot, count, keyboard key).
import type { ReactNode } from 'react';
import { CircleAlert, CircleCheck, Info, Loader2, TriangleAlert, X } from 'lucide-react';
import { cx, toneSoft, toneSolid, type Tone } from './foundation';

const alertIcon = { neutral: Info, info: Info, success: CircleCheck, warning: TriangleAlert, danger: CircleAlert };

/** A message in the page: says what happened and what to do; may offer one action. */
export function Alert({ tone = 'info', title, children, action, onClose, closeLabel }: { tone?: Tone; title?: ReactNode; children?: ReactNode; action?: ReactNode; onClose?: () => void; closeLabel?: string }) {
  const Icon = alertIcon[tone];
  return (
    <div role={tone === 'danger' ? 'alert' : 'status'} className={cx('flex items-start gap-3 rounded-xl px-4 py-3 text-sm', toneSoft[tone])}>
      <Icon size={17} aria-hidden className="mt-px shrink-0" />
      <div className="min-w-0 flex-1 leading-6">
        {title && <p className="font-semibold">{title}</p>}
        {children && <div className={title ? 'opacity-90' : undefined}>{children}</div>}
        {action && <div className="mt-2">{action}</div>}
      </div>
      {onClose && (
        <button type="button" onClick={onClose} aria-label={closeLabel} title={closeLabel} className="-m-1 rounded-md p-1 opacity-70 hover:opacity-100">
          <X size={15} aria-hidden />
        </button>
      )}
    </div>
  );
}

export function Spinner({ size = 16, label }: { size?: number; label?: string }) {
  return (
    <span role={label ? 'status' : undefined} className="inline-flex items-center gap-2 text-sm text-neutral-500 dark:text-neutral-400">
      <Loader2 size={size} className="animate-spin" aria-hidden />
      {label}
    </span>
  );
}

/** Progress of a known amount (value) or of unknown length (no value). */
export function ProgressBar({ value, label, tone = 'neutral' }: { value?: number; label: string; tone?: Tone }) {
  const known = value !== undefined;
  const fill = tone === 'neutral' ? 'bg-signal-500' : toneSolid[tone];
  return (
    <div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={known ? Math.round(value) : undefined} className="h-1.5 w-full overflow-hidden rounded-full bg-neutral-900/[0.08] dark:bg-white/[0.1]">
      <div className={cx('h-full rounded-full transition-[width]', fill, !known && 'w-1/3 animate-indeterminate')} style={known ? { width: `${Math.min(100, Math.max(0, value))}%` } : undefined} />
    </div>
  );
}

/** Placeholder shapes while content loads, where the content will be. */
export function Skeleton({ className }: { className?: string }) {
  return <span aria-hidden className={cx('block animate-shimmer rounded-md bg-neutral-900/[0.08] dark:bg-white/[0.08]', className)} />;
}

/** An empty place says why it is empty and what to do; one primary action at most. */
export function EmptyState({ icon, title, description, action }: { icon?: ReactNode; title: ReactNode; description?: ReactNode; action?: ReactNode }) {
  return (
    <div className="mx-auto flex max-w-sm flex-col items-center px-6 py-10 text-center">
      {icon && <div className="mb-4 text-neutral-400">{icon}</div>}
      <p className="text-sm font-semibold text-neutral-900 dark:text-neutral-50">{title}</p>
      {description && <p className="mt-1.5 text-sm leading-6 text-neutral-500 dark:text-neutral-400">{description}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

/** A short label of state or category. */
export function Badge({ tone = 'neutral', children, dot = false }: { tone?: Tone; children: ReactNode; dot?: boolean }) {
  return (
    <span className={cx('inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium leading-4', toneSoft[tone])}>
      {dot && <span className={cx('h-1.5 w-1.5 rounded-full', toneSolid[tone])} />}
      {children}
    </span>
  );
}

/** A removable value, e.g. a chosen filter. */
export function Tag({ children, onRemove, removeLabel }: { children: ReactNode; onRemove?: () => void; removeLabel?: string }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md bg-neutral-900/[0.06] py-0.5 pl-2 pr-1 text-xs text-neutral-700 dark:bg-white/[0.08] dark:text-neutral-200">
      {children}
      {onRemove && (
        <button type="button" onClick={onRemove} aria-label={removeLabel} title={removeLabel} className="rounded p-0.5 text-neutral-400 hover:bg-neutral-900/[0.08] hover:text-neutral-700 dark:hover:bg-white/[0.1] dark:hover:text-neutral-100">
          <X size={12} aria-hidden />
        </button>
      )}
    </span>
  );
}

/** A dot for a state; always paired with text or an accessible label. */
export function StatusDot({ tone, label, pulse = false }: { tone: Tone; label: string; pulse?: boolean }) {
  return (
    <span role="img" aria-label={label} title={label} className="relative inline-flex h-2.5 w-2.5 shrink-0">
      {pulse && <span className={cx('absolute inset-0 animate-ping rounded-full opacity-60', toneSolid[tone])} />}
      <span className={cx('relative h-2.5 w-2.5 rounded-full', toneSolid[tone])} />
    </span>
  );
}

/** A count on a tab, button or rail icon. */
export function Count({ value, tone = 'neutral', label }: { value: number; tone?: 'neutral' | 'danger'; label: string }) {
  return (
    <span aria-label={label} className={cx('inline-flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] font-semibold tabular-nums leading-none', tone === 'danger' ? 'bg-red-600 text-white' : 'bg-neutral-900 text-white dark:bg-neutral-100 dark:text-neutral-900')}>
      {value > 99 ? '99+' : value}
    </span>
  );
}

/** A key on the keyboard, in shortcut hints. */
export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="inline-flex h-5 min-w-5 items-center justify-center rounded border border-neutral-300 bg-white px-1 font-mono text-[11px] text-neutral-600 shadow-[0_1px_0_rgb(0_0_0/0.08)] dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-300">{children}</kbd>;
}
