// Select: choose one of a short, fixed list (language, model family). The
// list opens in a styled panel that matches every other floating panel.
// For long lists that need typing to find, use Combobox.
import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { Check, ChevronDown } from 'lucide-react';
import { controlHeight, cx, layer, surface, type ControlSize } from './foundation';
import { useField } from './Field';
import { controlFrame } from './controlFrame';
import { useDismiss } from './useDismiss';
import { placementClass, useFloatingPlacement } from './useFloating';

export interface SelectOption {
  value: string;
  label: string;
  description?: ReactNode;
  disabled?: boolean;
}

export function Select({ options, value, onChange, size = 'md', placeholder, label, disabled = false, invalid }: { options: SelectOption[]; value: string; onChange: (value: string) => void; size?: ControlSize; placeholder?: string; label?: string; disabled?: boolean; invalid?: boolean }) {
  const field = useField();
  const listId = useId();
  const root = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLUListElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const [open, setOpen] = useState(false);
  const selectedIndex = options.findIndex((option) => option.value === value);
  const [active, setActive] = useState(Math.max(selectedIndex, 0));
  const placement = useFloatingPlacement(panel, open, 'bottom-start');
  const isInvalid = invalid ?? field?.invalid ?? false;
  useDismiss(root, open, () => setOpen(false));

  const enabled = (index: number) => !options[index]?.disabled;
  const step = (from: number, by: number) => {
    for (let i = 1; i <= options.length; i++) {
      const next = (from + by * i + options.length) % options.length;
      if (enabled(next)) return next;
    }
    return from;
  };
  const choose = (index: number) => {
    if (!enabled(index)) return;
    onChange(options[index].value);
    setOpen(false);
    trigger.current?.focus();
  };
  const openAt = (index: number) => {
    setActive(index);
    setOpen(true);
  };
  const onKeyDown = (event: KeyboardEvent) => {
    if (!open) {
      if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(event.key)) {
        event.preventDefault();
        openAt(Math.max(selectedIndex, 0));
      }
      return;
    }
    if (event.key === 'ArrowDown') setActive((at) => step(at, 1));
    else if (event.key === 'ArrowUp') setActive((at) => step(at, -1));
    else if (event.key === 'Home') setActive(step(-1, 1));
    else if (event.key === 'End') setActive(step(options.length, -1));
    else if (event.key === 'Enter' || event.key === ' ') choose(active);
    else if (event.key === 'Tab') setOpen(false);
    else if (event.key.length === 1) {
      // Typing a letter jumps to the next option starting with it.
      const letter = event.key.toLowerCase();
      const hit = options.findIndex((option, index) => index > active && option.label.toLowerCase().startsWith(letter));
      const wrap = options.findIndex((option) => option.label.toLowerCase().startsWith(letter));
      if (hit >= 0 || wrap >= 0) setActive(hit >= 0 ? hit : wrap);
      return;
    } else return;
    event.preventDefault();
  };
  const selected = options[selectedIndex];

  return (
    <div ref={root} className="relative">
      <button
        ref={trigger}
        id={field?.id}
        type="button"
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={open ? `${listId}-${active}` : undefined}
        aria-label={label}
        aria-describedby={field?.describedBy}
        aria-invalid={isInvalid || undefined}
        disabled={disabled}
        onClick={() => (open ? setOpen(false) : openAt(Math.max(selectedIndex, 0)))}
        onKeyDown={onKeyDown}
        className={cx('flex w-full items-center gap-2 px-3 text-left disabled:cursor-not-allowed disabled:opacity-60', controlFrame, controlHeight[size], isInvalid && 'border-red-500 hover:border-red-500')}
      >
        <span className={cx('min-w-0 flex-1 truncate', !selected && 'text-neutral-400')}>{selected?.label ?? placeholder}</span>
        <ChevronDown size={16} aria-hidden className={cx('shrink-0 text-neutral-400 transition-transform', open && 'rotate-180')} />
      </button>
      {open && (
        <ul ref={panel} id={listId} role="listbox" aria-label={label} className={cx('absolute min-w-full max-h-72 animate-pop-in overflow-y-auto rounded-xl p-1', surface.overlay, layer.dropdown, placementClass[placement])}>
          {options.map((option, index) => (
            <li
              key={option.value}
              id={`${listId}-${index}`}
              role="option"
              aria-selected={option.value === value}
              aria-disabled={option.disabled || undefined}
              onMouseEnter={() => enabled(index) && setActive(index)}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => choose(index)}
              className={cx('flex cursor-pointer items-start gap-2 whitespace-nowrap rounded-lg px-2.5 py-1.5 text-sm', index === active && 'bg-neutral-900/[0.06] dark:bg-white/[0.08]', option.disabled && 'cursor-not-allowed opacity-40')}
            >
              <Check size={15} aria-hidden className={cx('mt-0.5 shrink-0', option.value === value ? 'opacity-100' : 'opacity-0')} />
              <span className="min-w-0">
                <span className="block text-neutral-900 dark:text-neutral-50">{option.label}</span>
                {option.description && <span className="block whitespace-normal text-xs text-neutral-500 dark:text-neutral-400">{option.description}</span>}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
