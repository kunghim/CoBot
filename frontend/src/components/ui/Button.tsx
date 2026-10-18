// Buttons: the one way to make something clickable that does something.
// A button says what it does ("Save changes", not "OK"); only one primary
// button per view; danger is only for actions that cannot be undone.
import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react';
import { Loader2 } from 'lucide-react';
import { controlHeight, cx, interactive, type ControlSize } from './foundation';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'link';

const variantClass: Record<ButtonVariant, string> = {
  primary: 'bg-neutral-900 text-white shadow-sm hover:bg-neutral-700 active:bg-neutral-800 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300',
  secondary: `bg-white text-neutral-800 shadow-sm ring-1 ring-inset ring-neutral-300 hover:bg-neutral-50 active:bg-neutral-100 dark:bg-neutral-900 dark:text-neutral-100 dark:ring-neutral-700 dark:hover:bg-neutral-800`,
  ghost: `text-neutral-700 dark:text-neutral-200 ${interactive.hover} ${interactive.pressed}`,
  danger: 'bg-red-600 text-white shadow-sm hover:bg-red-700 active:bg-red-800',
  link: 'h-auto px-0 text-signal-600 underline-offset-4 hover:underline dark:text-signal-400',
};

const padding: Record<ControlSize, string> = { sm: 'px-2.5 gap-1.5', md: 'px-3.5 gap-2', lg: 'px-5 gap-2' };
const iconSize: Record<ControlSize, number> = { sm: 14, md: 16, lg: 18 };

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ControlSize;
  /** Shown while the action runs; the button is disabled meanwhile. */
  loading?: boolean;
  /** An icon component (lucide) before the label. */
  icon?: (props: { size?: number; 'aria-hidden'?: boolean }) => ReactNode;
  /** An icon after the label, e.g. a chevron for menus. */
  trailingIcon?: (props: { size?: number; 'aria-hidden'?: boolean }) => ReactNode;
  block?: boolean;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = 'secondary', size = 'md', loading = false, icon: Icon, trailingIcon: Trailing, block = false, className, children, disabled, type = 'button', ...rest },
  ref,
) {
  const px = iconSize[size];
  return (
    <button
      ref={ref}
      type={type}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={cx(
        'inline-flex shrink-0 select-none items-center justify-center rounded-lg font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        variant !== 'link' && controlHeight[size],
        variant !== 'link' && padding[size],
        variant === 'link' && (size === 'sm' ? 'text-xs' : 'text-sm'),
        variantClass[variant],
        block && 'w-full',
        className,
      )}
      {...rest}
    >
      {loading ? <Loader2 size={px} className="animate-spin" aria-hidden /> : Icon && <Icon size={px} aria-hidden />}
      {children}
      {Trailing && <Trailing size={px} aria-hidden />}
    </button>
  );
});

export interface IconButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'children'> {
  /** Required: what the button does, read by screen readers and shown as a tooltip. */
  label: string;
  icon: (props: { size?: number; 'aria-hidden'?: boolean }) => ReactNode;
  size?: ControlSize;
  variant?: 'ghost' | 'secondary' | 'primary';
  /** For toggles (a panel open, a filter on). */
  pressed?: boolean;
}

const square: Record<ControlSize, string> = { sm: 'h-7 w-7', md: 'h-9 w-9', lg: 'h-11 w-11' };

/** A button that is only an icon. It always has a label and a tooltip. */
export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { label, icon: Icon, size = 'md', variant = 'ghost', pressed, className, type = 'button', ...rest },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      aria-label={label}
      title={label}
      aria-pressed={pressed}
      className={cx(
        'inline-flex shrink-0 items-center justify-center rounded-lg transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        square[size],
        variant === 'ghost' && `text-neutral-500 hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100 ${interactive.hover} ${interactive.pressed}`,
        variant === 'secondary' && variantClass.secondary,
        variant === 'primary' && variantClass.primary,
        pressed && interactive.selected,
        className,
      )}
      {...rest}
    >
      <Icon size={iconSize[size]} aria-hidden />
    </button>
  );
});

/** Buttons that belong together, joined into one control. */
export function ButtonGroup({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div role="group" aria-label={label} className={cx('inline-flex [&>button]:rounded-none [&>button:first-child]:rounded-l-lg [&>button:last-child]:rounded-r-lg [&>button+button]:-ml-px', className)}>
      {children}
    </div>
  );
}
