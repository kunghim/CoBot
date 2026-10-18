// Text entry: single line, multi line, search and numbers, plus the native
// select. Inside a Field they take its id and descriptions automatically.
import { forwardRef, useLayoutEffect, useRef, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes, type TextareaHTMLAttributes } from 'react';
import { ChevronDown, Minus, Plus, Search, X } from 'lucide-react';
import { controlHeight, cx, interactive, type ControlSize } from './foundation';
import { useField } from './Field';

/** The frame every text control shares: border, focus ring, invalid state. */
export const controlFrame =
  'rounded-lg border border-neutral-300 bg-white text-neutral-900 shadow-sm transition-colors placeholder:text-neutral-400 hover:border-neutral-400 focus-within:border-signal-500 focus-within:ring-2 focus-within:ring-signal-500/25 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-100 dark:hover:border-neutral-600';
const invalidFrame = 'border-red-500 hover:border-red-500 focus-within:border-red-500 focus-within:ring-red-500/25 dark:border-red-500';
const disabledFrame = 'opacity-60 cursor-not-allowed';

export interface TextInputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, 'size' | 'prefix'> {
  size?: ControlSize;
  /** Content before the text, e.g. an icon or a unit. */
  prefix?: ReactNode;
  /** Content after the text, e.g. a unit or an action. */
  suffix?: ReactNode;
  invalid?: boolean;
  /** Machine values (paths, ids, keys) use the monospace face. */
  mono?: boolean;
}

export const TextInput = forwardRef<HTMLInputElement, TextInputProps>(function TextInput(
  { size = 'md', prefix, suffix, invalid, mono, className, id, disabled, ...rest },
  ref,
) {
  const field = useField();
  const isInvalid = invalid ?? field?.invalid ?? false;
  return (
    <div className={cx('flex items-center gap-2 px-3', controlFrame, controlHeight[size], isInvalid && invalidFrame, disabled && disabledFrame, className)}>
      {prefix && <span className="flex shrink-0 items-center text-neutral-400">{prefix}</span>}
      <input
        ref={ref}
        id={id ?? field?.id}
        aria-describedby={field?.describedBy}
        aria-invalid={isInvalid || undefined}
        disabled={disabled}
        className={cx('min-w-0 flex-1 bg-transparent outline-none disabled:cursor-not-allowed', mono && 'font-mono text-[0.92em]')}
        {...rest}
      />
      {suffix && <span className="flex shrink-0 items-center text-neutral-500">{suffix}</span>}
    </div>
  );
});

export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  invalid?: boolean;
  /** Grow with the text up to this many pixels. */
  autoGrowMax?: number;
}

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { invalid, autoGrowMax, className, id, value, ...rest },
  forwarded,
) {
  const field = useField();
  const inner = useRef<HTMLTextAreaElement | null>(null);
  useLayoutEffect(() => {
    const element = inner.current;
    if (!element || !autoGrowMax) return;
    element.style.height = 'auto';
    element.style.height = `${Math.min(element.scrollHeight, autoGrowMax)}px`;
  }, [value, autoGrowMax]);
  const isInvalid = invalid ?? field?.invalid ?? false;
  return (
    <div className={cx(controlFrame, 'px-3 py-2', isInvalid && invalidFrame, rest.disabled && disabledFrame, className)}>
      <textarea
        ref={(element) => {
          inner.current = element;
          if (typeof forwarded === 'function') forwarded(element);
          else if (forwarded) forwarded.current = element;
        }}
        id={id ?? field?.id}
        value={value}
        aria-describedby={field?.describedBy}
        aria-invalid={isInvalid || undefined}
        className="block min-h-[3.5rem] w-full resize-none bg-transparent text-sm leading-6 outline-none disabled:cursor-not-allowed"
        {...rest}
      />
    </div>
  );
});

/** A search box: an icon, the query and a clear button once there is text. */
export function SearchInput({ value, onChange, placeholder, label, clearLabel, size = 'md', className }: { value: string; onChange: (value: string) => void; placeholder?: string; label: string; clearLabel: string; size?: ControlSize; className?: string }) {
  return (
    <TextInput
      type="search"
      size={size}
      value={value}
      aria-label={label}
      placeholder={placeholder}
      onChange={(event) => onChange(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === 'Escape' && value) {
          event.preventDefault();
          onChange('');
        }
      }}
      prefix={<Search size={15} aria-hidden />}
      suffix={
        value ? (
          <button type="button" aria-label={clearLabel} title={clearLabel} onClick={() => onChange('')} className={cx('rounded p-0.5 text-neutral-400 hover:text-neutral-700 dark:hover:text-neutral-200', interactive.hover)}>
            <X size={14} aria-hidden />
          </button>
        ) : null
      }
      className={cx('[&_input::-webkit-search-cancel-button]:hidden', className)}
    />
  );
}

/** A whole number with steppers; the value is clamped on commit. */
export function NumberInput({ value, onChange, min, max, step = 1, size = 'md', unit, id, disabled, decreaseLabel, increaseLabel }: { value: number; onChange: (value: number) => void; min?: number; max?: number; step?: number; size?: ControlSize; unit?: string; id?: string; disabled?: boolean; decreaseLabel: string; increaseLabel: string }) {
  const clamp = (next: number) => Math.min(max ?? Infinity, Math.max(min ?? -Infinity, next));
  const stepButton = 'flex h-full items-center px-2 text-neutral-500 hover:text-neutral-900 disabled:opacity-40 dark:hover:text-neutral-100';
  return (
    <TextInput
      id={id}
      size={size}
      type="number"
      inputMode="numeric"
      value={Number.isFinite(value) ? value : ''}
      min={min}
      max={max}
      step={step}
      disabled={disabled}
      onChange={(event) => onChange(clamp(Number(event.target.value)))}
      className="pr-0 [&_input]:[appearance:textfield] [&_input::-webkit-inner-spin-button]:appearance-none"
      suffix={
        <span className="flex h-full items-center">
          {unit && <span className="mr-2 text-xs">{unit}</span>}
          <button type="button" aria-label={decreaseLabel} title={decreaseLabel} disabled={disabled || (min !== undefined && value <= min)} onClick={() => onChange(clamp(value - step))} className={stepButton}>
            <Minus size={14} aria-hidden />
          </button>
          <button type="button" aria-label={increaseLabel} title={increaseLabel} disabled={disabled || (max !== undefined && value >= max)} onClick={() => onChange(clamp(value + step))} className={stepButton}>
            <Plus size={14} aria-hidden />
          </button>
        </span>
      }
    />
  );
}

export interface SelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, 'size'> {
  size?: ControlSize;
  invalid?: boolean;
  options: Array<{ value: string; label: string; disabled?: boolean }>;
}

/** The native select: right for short, fixed lists; it works everywhere. */
export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { size = 'md', invalid, options, className, id, ...rest },
  ref,
) {
  const field = useField();
  const isInvalid = invalid ?? field?.invalid ?? false;
  return (
    <div className={cx('relative', controlFrame, controlHeight[size], isInvalid && invalidFrame, rest.disabled && disabledFrame, className)}>
      <select
        ref={ref}
        id={id ?? field?.id}
        aria-describedby={field?.describedBy}
        aria-invalid={isInvalid || undefined}
        className="h-full w-full cursor-pointer appearance-none rounded-lg bg-transparent pl-3 pr-9 outline-none disabled:cursor-not-allowed"
        {...rest}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value} disabled={option.disabled}>
            {option.label}
          </option>
        ))}
      </select>
      <ChevronDown size={16} aria-hidden className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-neutral-400" />
    </div>
  );
});
