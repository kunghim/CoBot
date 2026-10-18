// Combobox: choose one of many options by typing to filter (models, roles,
// directories). For a short fixed list use Select or SegmentedControl.
import { useId, useMemo, useRef, useState, type ReactNode } from 'react';
import { Check, ChevronsUpDown } from 'lucide-react';
import { controlHeight, cx, layer, surface, type ControlSize } from './foundation';
import { controlFrame } from './controlFrame';
import { useField } from './Field';
import { useDismiss } from './useDismiss';
import { placementClass, useFloatingPlacement } from './useFloating';

export interface ComboboxOption {
  value: string;
  label: string;
  description?: ReactNode;
}

export function Combobox({ options, value, onChange, placeholder, emptyText, size = 'md', label }: { options: ComboboxOption[]; value: string; onChange: (value: string) => void; placeholder?: string; emptyText: string; size?: ControlSize; label?: string }) {
  const field = useField();
  const listId = useId();
  const root = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLUListElement>(null);
  const [open, setOpen] = useState(false);
  const placement = useFloatingPlacement(panel, open, 'bottom-start');
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const selected = options.find((option) => option.value === value);
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? options.filter((option) => option.label.toLowerCase().includes(q) || option.value.toLowerCase().includes(q)) : options;
  }, [options, query]);
  useDismiss(root, open, () => setOpen(false));

  const choose = (option: ComboboxOption) => {
    onChange(option.value);
    setQuery('');
    setOpen(false);
  };

  return (
    <div ref={root} className="relative">
      <div className={cx('flex items-center gap-2 px-3', controlFrame, controlHeight[size])}>
        <input
          id={field?.id}
          role="combobox"
          aria-label={label}
          aria-expanded={open}
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={open && shown[active] ? `${listId}-${active}` : undefined}
          aria-describedby={field?.describedBy}
          value={open ? query : (selected?.label ?? '')}
          placeholder={selected?.label ?? placeholder}
          onFocus={() => setOpen(true)}
          onChange={(event) => {
            setQuery(event.target.value);
            setActive(0);
            setOpen(true);
          }}
          onKeyDown={(event) => {
            if (event.key === 'ArrowDown') {
              event.preventDefault();
              setOpen(true);
              setActive((at) => Math.min(at + 1, shown.length - 1));
            } else if (event.key === 'ArrowUp') {
              event.preventDefault();
              setActive((at) => Math.max(at - 1, 0));
            } else if (event.key === 'Enter' && open && shown[active]) {
              event.preventDefault();
              choose(shown[active]);
            } else if (event.key === 'Escape' && open) {
              event.preventDefault();
              setOpen(false);
              setQuery('');
            }
          }}
          className="min-w-0 flex-1 bg-transparent outline-none"
        />
        <ChevronsUpDown size={15} aria-hidden className="shrink-0 text-neutral-400" />
      </div>
      {open && (
        <ul ref={panel} id={listId} role="listbox" className={cx('absolute right-0 max-h-64 animate-pop-in overflow-y-auto rounded-xl p-1', surface.overlay, layer.dropdown, placementClass[placement])}>
          {shown.length === 0 && <li className="px-3 py-2 text-sm text-neutral-500">{emptyText}</li>}
          {shown.map((option, index) => (
            <li
              key={option.value}
              id={`${listId}-${index}`}
              role="option"
              aria-selected={option.value === value}
              onMouseEnter={() => setActive(index)}
              onMouseDown={(event) => event.preventDefault()}
              onClick={() => choose(option)}
              className={cx('flex cursor-pointer items-start gap-2 rounded-lg px-3 py-2 text-sm', index === active && 'bg-neutral-900/[0.06] dark:bg-white/[0.08]')}
            >
              <Check size={15} aria-hidden className={cx('mt-0.5 shrink-0', option.value === value ? 'opacity-100' : 'opacity-0')} />
              <span className="min-w-0">
                <span className="block text-neutral-900 dark:text-neutral-50">{option.label}</span>
                {option.description && <span className="block text-xs text-neutral-500 dark:text-neutral-400">{option.description}</span>}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
