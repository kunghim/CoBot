// Choosing: checkbox (independent yes/no that applies on save), switch
// (a setting that applies at once), radios and radio cards (one of a few,
// all visible), checkbox cards (several of a few, with pictures), segmented control (one of 2–4 short options) and toggle.
import { useId, useRef, type KeyboardEvent, type ReactNode } from 'react';
import { Check, Loader2, Lock, Minus, PlugZap, RotateCcw } from 'lucide-react';
import { controlHeight, cx, interactive, type ControlSize } from './foundation';

export function Checkbox({
  checked,
  onChange,
  label,
  description,
  indeterminate = false,
  disabled = false,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: ReactNode;
  description?: ReactNode;
  indeterminate?: boolean;
  disabled?: boolean;
}) {
  const id = useId();
  const on = checked || indeterminate;
  return (
    <label htmlFor={id} className={cx('group flex items-start gap-2.5 text-sm', disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer')}>
      <span className="relative mt-0.5 flex h-4 w-4 shrink-0">
        <input
          id={id}
          type="checkbox"
          checked={checked}
          disabled={disabled}
          ref={(element) => {
            if (element) element.indeterminate = indeterminate;
          }}
          aria-describedby={description ? `${id}-description` : undefined}
          onChange={(event) => onChange(event.target.checked)}
          className="peer absolute inset-0 cursor-[inherit] opacity-0"
        />
        <span
          aria-hidden
          className={cx(
            'flex h-4 w-4 items-center justify-center rounded border transition-colors peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-signal-500',
            on ? 'border-neutral-900 bg-neutral-900 text-white dark:border-neutral-100 dark:bg-neutral-100 dark:text-neutral-900' : 'border-neutral-400 bg-white group-hover:border-neutral-600 dark:border-neutral-600 dark:bg-neutral-900',
          )}
        >
          {indeterminate ? <Minus size={12} strokeWidth={3} /> : checked ? <Check size={12} strokeWidth={3} /> : null}
        </span>
      </span>
      <span className="min-w-0">
        <span className="block text-neutral-800 dark:text-neutral-100">{label}</span>
        {description && <span id={`${id}-description`} className="mt-0.5 block text-xs leading-5 text-neutral-500 dark:text-neutral-400">{description}</span>}
      </span>
    </label>
  );
}

export function Switch({ checked, onChange, label, description, disabled = false, hideLabel = false }: { checked: boolean; onChange: (checked: boolean) => void; label: ReactNode; description?: ReactNode; disabled?: boolean; hideLabel?: boolean }) {
  const id = useId();
  return (
    <div className={cx('flex items-start justify-between gap-4', disabled && 'opacity-50')}>
      <label htmlFor={id} className={cx('min-w-0 text-sm', hideLabel && 'sr-only')}>
        <span className="block text-neutral-800 dark:text-neutral-100">{label}</span>
        {description && <span className="mt-0.5 block text-xs leading-5 text-neutral-500 dark:text-neutral-400">{description}</span>}
      </label>
      <button
        id={id}
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        onClick={() => onChange(!checked)}
        className={cx(
          'relative mt-0.5 inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:cursor-not-allowed',
          checked ? 'bg-signal-600 dark:bg-signal-500' : 'bg-neutral-300 dark:bg-neutral-700',
        )}
      >
        <span className={cx('inline-block h-4 w-4 rounded-full shadow transition-transform', checked ? 'translate-x-[18px] bg-on-signal' : 'translate-x-0.5 bg-white')} />
      </button>
    </div>
  );
}

export interface ChoiceOption<T extends string> {
  value: T;
  label: ReactNode;
  description?: ReactNode;
  disabled?: boolean;
  /** Visual shown on a radio card (an avatar, a swatch). */
  media?: ReactNode;
}

/** Arrow keys move the choice, as native radio groups do. */
function useRovingRadio<T extends string>(options: ChoiceOption<T>[], value: T, onChange: (value: T) => void) {
  const refs = useRef<Array<HTMLElement | null>>([]);
  const onKeyDown = (event: KeyboardEvent) => {
    const enabled = options.map((option, index) => ({ option, index })).filter(({ option }) => !option.disabled);
    const at = enabled.findIndex(({ option }) => option.value === value);
    let next: number;
    if (event.key === 'ArrowRight' || event.key === 'ArrowDown') next = (at + 1) % enabled.length;
    else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') next = (at - 1 + enabled.length) % enabled.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = enabled.length - 1;
    else return;
    event.preventDefault();
    const target = enabled[next];
    onChange(target.option.value);
    refs.current[target.index]?.focus();
  };
  return { refs, onKeyDown };
}

export function RadioGroup<T extends string>({ label, options, value, onChange, orientation = 'vertical' }: { label: string; options: ChoiceOption<T>[]; value: T; onChange: (value: T) => void; orientation?: 'vertical' | 'horizontal' }) {
  const { refs, onKeyDown } = useRovingRadio(options, value, onChange);
  return (
    <div role="radiogroup" aria-label={label} onKeyDown={onKeyDown} className={cx('flex gap-3', orientation === 'vertical' ? 'flex-col' : 'flex-wrap')}>
      {options.map((option, index) => {
        const selected = option.value === value;
        return (
          <button
            key={option.value}
            ref={(element) => {
              refs.current[index] = element;
            }}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={selected || (index === 0 && !options.some((entry) => entry.value === value)) ? 0 : -1}
            disabled={option.disabled}
            onClick={() => onChange(option.value)}
            className="group flex items-start gap-2.5 text-left text-sm disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span className={cx('mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full border transition-colors', selected ? 'border-neutral-900 dark:border-neutral-100' : 'border-neutral-400 group-hover:border-neutral-600 dark:border-neutral-600')}>
              {selected && <span className="h-2 w-2 rounded-full bg-neutral-900 dark:bg-neutral-100" />}
            </span>
            <span className="min-w-0">
              <span className="block text-neutral-800 dark:text-neutral-100">{option.label}</span>
              {option.description && <span className="mt-0.5 block text-xs leading-5 text-neutral-500 dark:text-neutral-400">{option.description}</span>}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/** Radios as cards: for choices that need a picture or a description. */
export function RadioCards<T extends string>({ label, options, value, onChange, columns = 2 }: { label: string; options: ChoiceOption<T>[]; value: T; onChange: (value: T) => void; columns?: 2 | 3 | 4 }) {
  const { refs, onKeyDown } = useRovingRadio(options, value, onChange);
  const grid = { 2: 'sm:grid-cols-2', 3: 'sm:grid-cols-3', 4: 'sm:grid-cols-4' }[columns];
  return (
    <div role="radiogroup" aria-label={label} onKeyDown={onKeyDown} className={cx('grid gap-3', grid)}>
      {options.map((option, index) => {
        const selected = option.value === value;
        return (
          <button
            key={option.value}
            ref={(element) => {
              refs.current[index] = element;
            }}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={selected || (index === 0 && !options.some((entry) => entry.value === value)) ? 0 : -1}
            disabled={option.disabled}
            onClick={() => onChange(option.value)}
            className={cx(
              'flex items-start gap-3 rounded-xl border-2 p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-50',
              selected ? 'border-signal-500 bg-signal-500/[0.04]' : 'border-neutral-200 hover:border-neutral-300 dark:border-neutral-700 dark:hover:border-neutral-600',
            )}
          >
            {option.media}
            <span className="min-w-0">
              <span className="block text-sm font-medium text-neutral-900 dark:text-neutral-50">{option.label}</span>
              {option.description && <span className="mt-0.5 block text-xs leading-5 text-neutral-500 dark:text-neutral-400">{option.description}</span>}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/**
 * Checkboxes as cards: several choices that each need a picture or a
 * description (the members of a new team). A locked option is part of the
 * choice and cannot be removed; lockedLabel says why.
 */
export function CheckboxCards<T extends string>({ label, options, values, onToggle, locked = [], lockedLabel, columns = 2 }: { label: string; options: ChoiceOption<T>[]; values: ReadonlySet<T>; onToggle: (value: T) => void; locked?: T[]; lockedLabel?: ReactNode; columns?: 2 | 3 }) {
  const grid = { 2: 'sm:grid-cols-2', 3: 'sm:grid-cols-3' }[columns];
  return (
    <div role="group" aria-label={label} className={cx('grid gap-2.5', grid)}>
      {options.map((option) => {
        const isLocked = locked.includes(option.value);
        const checked = isLocked || values.has(option.value);
        return (
          <button
            key={option.value}
            type="button"
            role="checkbox"
            aria-checked={checked}
            aria-disabled={isLocked || option.disabled || undefined}
            disabled={option.disabled}
            onClick={() => !isLocked && onToggle(option.value)}
            className={cx(
              'flex items-start gap-2.5 rounded-xl border-2 p-2.5 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-50',
              checked ? 'border-signal-500 bg-signal-500/[0.04]' : 'border-neutral-200 hover:border-neutral-300 dark:border-neutral-700 dark:hover:border-neutral-600',
              isLocked && 'cursor-default',
            )}
          >
            <span aria-hidden className={cx('mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded border transition-colors', checked ? 'border-signal-500 bg-signal-500 text-on-signal' : 'border-neutral-300 dark:border-neutral-600')}>
              {isLocked ? <Lock size={10} /> : checked && <Check size={12} strokeWidth={3} />}
            </span>
            {option.media}
            <span className="min-w-0">
              <span className="flex items-center gap-1.5 text-sm font-medium text-neutral-900 dark:text-neutral-50">
                {option.label}
                {isLocked && lockedLabel && <span className="rounded bg-neutral-900/[0.06] px-1 text-2xs font-normal text-neutral-600 dark:bg-white/[0.08] dark:text-neutral-300">{lockedLabel}</span>}
              </span>
              {option.description && <span className="mt-0.5 block text-xs leading-5 text-neutral-500 dark:text-neutral-400">{option.description}</span>}
            </span>
          </button>
        );
      })}
    </div>
  );
}

/** One of 2–4 short options, side by side; applies at once. */
export function SegmentedControl<T extends string>({ label, options, value, onChange, size = 'md' }: { label: string; options: Array<ChoiceOption<T> & { icon?: (props: { size?: number }) => ReactNode }>; value: T; onChange: (value: T) => void; size?: ControlSize }) {
  const { refs, onKeyDown } = useRovingRadio(options, value, onChange);
  return (
    <div role="radiogroup" aria-label={label} onKeyDown={onKeyDown} className={cx('inline-flex gap-1 rounded-xl bg-neutral-900/[0.06] p-1 dark:bg-white/[0.06]')}>
      {options.map((option, index) => {
        const selected = option.value === value;
        const Icon = option.icon;
        return (
          <button
            key={option.value}
            ref={(element) => {
              refs.current[index] = element;
            }}
            type="button"
            role="radio"
            aria-checked={selected}
            tabIndex={selected || (index === 0 && !options.some((entry) => entry.value === value)) ? 0 : -1}
            disabled={option.disabled}
            onClick={() => onChange(option.value)}
            className={cx(
              'inline-flex items-center gap-1.5 rounded-lg px-3 font-medium transition-colors disabled:opacity-50',
              controlHeight[size === 'lg' ? 'md' : 'sm'],
              selected ? 'bg-white text-neutral-900 shadow-sm dark:bg-neutral-700 dark:text-neutral-50' : 'text-neutral-600 hover:text-neutral-900 dark:text-neutral-300 dark:hover:text-neutral-50',
            )}
          >
            {Icon && <Icon size={14} />}
            {option.label}
          </button>
        );
      })}
    </div>
  );
}

/** A button that stays on or off, e.g. a filter. */
export function Toggle({ pressed, onChange, children, size = 'md', icon: Icon }: { pressed: boolean; onChange: (pressed: boolean) => void; children: ReactNode; size?: ControlSize; icon?: (props: { size?: number }) => ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      onClick={() => onChange(!pressed)}
      className={cx(
        'inline-flex items-center gap-1.5 rounded-lg border px-3 font-medium transition-colors',
        controlHeight[size],
        pressed ? `border-transparent ${interactive.selected}` : `border-neutral-300 text-neutral-600 dark:border-neutral-700 dark:text-neutral-300 ${interactive.hover}`,
      )}
    >
      {Icon && <Icon size={14} />}
      {children}
    </button>
  );
}

/**
 * A small toggle in a wrapped row: pick several out of many (the models of
 * a provider, a set of filters). The chosen state is not carried by color
 * alone — a check mark marks it, and the chip reads its own state.
 */
export function Chip({
  pressed,
  onClick,
  children,
  trailing,
  mono = false,
  tone = 'neutral',
  disabled = false,
  title,
  className,
  action,
  centered = false,
  mark = true,
}: {
  pressed: boolean;
  onClick: () => void;
  children: ReactNode;
  /** A small marker on the right, e.g. a status badge. */
  trailing?: ReactNode;
  /** Machine values (ids, paths) use the monospace face. */
  mono?: boolean;
  /** danger: the chip reports a broken thing, so it reads red when chosen. */
  tone?: 'neutral' | 'danger';
  disabled?: boolean;
  title?: string;
  className?: string;
  /**
   * A second control on the chip that does something to the item without
   * choosing it (check it, retry it). It shows while the pointer is over the
   * chip or focus is in it, and stays shown when `visible` is set, so it can
   * also be reached without a mouse.
   */
  action?: { label: string; onClick: () => void; busy?: boolean; visible?: boolean; kind?: 'check' | 'retry' };
  /** Short fixed labels in a row: the text sits in the middle, the check mark's room balanced on both sides. */
  centered?: boolean;
  /** Whether the chosen state also shows a leading check mark. Off where the check mark means something else (a model that passed its check). */
  mark?: boolean;
}) {
  const chip = (
    <button
      type="button"
      aria-pressed={pressed}
      disabled={disabled}
      onClick={onClick}
      title={title}
      className={cx(
        'inline-flex max-w-full items-center gap-1.5 rounded-lg border py-1 text-xs transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        mark ? 'pl-2.5' : 'pl-3',
        centered && 'justify-center',
        action ? 'pr-7' : 'pr-2.5',
        mono && 'font-mono',
        pressed
          ? tone === 'danger'
            ? 'border-red-500 bg-red-500/10 text-red-700 dark:text-red-300'
            : 'border-signal-500 bg-signal-500/10 text-signal-600 dark:text-signal-400'
          : 'border-neutral-200 text-neutral-600 hover:border-neutral-300 hover:bg-neutral-900/[0.04] dark:border-neutral-700 dark:text-neutral-300 dark:hover:bg-white/[0.07]',
        !action && className,
      )}
    >
      {mark && (
        <span aria-hidden className={cx('text-[9px] leading-none', !pressed && 'opacity-0')}>
          ✓
        </span>
      )}
      <span className="min-w-0 truncate">{children}</span>
      {centered && mark && (
        <span aria-hidden className="text-[9px] leading-none opacity-0">
          ✓
        </span>
      )}
      {trailing}
    </button>
  );
  if (!action) return chip;
  return (
    <span className={cx('group relative inline-flex max-w-full', className)}>
      {chip}
      <button
        type="button"
        aria-label={action.label}
        title={action.label}
        disabled={disabled || action.busy}
        onClick={action.onClick}
        className={cx(
          'absolute right-1 top-1/2 inline-flex h-5 w-5 -translate-y-1/2 items-center justify-center rounded-md text-neutral-500 transition-opacity hover:bg-neutral-900/[0.08] hover:text-neutral-900 focus-visible:opacity-100 disabled:cursor-not-allowed dark:text-neutral-400 dark:hover:bg-white/[0.12] dark:hover:text-neutral-50',
          action.visible || action.busy ? 'opacity-100' : 'opacity-0 group-focus-within:opacity-100 group-hover:opacity-100',
        )}
      >
        {action.busy ? (
          <Loader2 size={12} className="animate-spin" aria-hidden />
        ) : action.kind === 'retry' ? (
          <RotateCcw size={12} aria-hidden />
        ) : (
          <PlugZap size={12} aria-hidden />
        )}
      </button>
    </span>
  );
}
