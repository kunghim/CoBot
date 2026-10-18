// Showing data: cards, lists, tables, key–value lists, code, statistics and
// dividers. Machine values (paths, ids, numbers) use the monospace face.
import { useId, useState, type ReactNode } from 'react';
import { ArrowDown, ArrowUp, ArrowUpDown, Check, ChevronDown, Copy } from 'lucide-react';
import { cx, interactive, surface } from './foundation';

/** A raised group of related content. */
export function Card({ title, description, action, children, padded = true, className }: { title?: ReactNode; description?: ReactNode; action?: ReactNode; children?: ReactNode; padded?: boolean; className?: string }) {
  return (
    <section className={cx('rounded-xl border border-neutral-200 dark:border-neutral-800', surface.raised, className)}>
      {(title || action) && (
        <header className="flex items-start gap-3 border-b border-neutral-200 px-5 py-3.5 dark:border-neutral-800">
          <div className="min-w-0 flex-1">
            {title && <h3 className="text-sm font-semibold text-neutral-900 dark:text-neutral-50">{title}</h3>}
            {description && <p className="mt-0.5 text-xs leading-5 text-neutral-500 dark:text-neutral-400">{description}</p>}
          </div>
          {action}
        </header>
      )}
      {children && <div className={padded ? 'p-5' : undefined}>{children}</div>}
    </section>
  );
}

/** Rows of similar things separated by hairlines. */
export function List({ children, label }: { children: ReactNode; label?: string }) {
  return <ul aria-label={label} className="divide-y divide-neutral-200 dark:divide-neutral-800">{children}</ul>;
}

/** One row: a leading visual, a title and detail, and trailing actions. */
export function ListRow({ leading, title, description, trailing, onClick, selected = false }: { leading?: ReactNode; title: ReactNode; description?: ReactNode; trailing?: ReactNode; onClick?: () => void; selected?: boolean }) {
  const body = (
    <>
      {leading}
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium text-neutral-900 dark:text-neutral-50">{title}</span>
        {description && <span className="mt-0.5 block truncate text-xs text-neutral-500 dark:text-neutral-400">{description}</span>}
      </span>
    </>
  );
  return (
    <li className="flex items-center gap-3 py-2.5">
      {onClick ? (
        <button type="button" onClick={onClick} aria-current={selected || undefined} className={cx('-mx-2 flex min-w-0 flex-1 items-center gap-3 rounded-lg px-2 py-1 text-left transition-colors', selected ? interactive.selected : interactive.hover)}>
          {body}
        </button>
      ) : (
        <span className="flex min-w-0 flex-1 items-center gap-3">{body}</span>
      )}
      {trailing && <span className="flex shrink-0 items-center gap-2">{trailing}</span>}
    </li>
  );
}

export interface Column<Row> {
  key: string;
  header: ReactNode;
  cell: (row: Row) => ReactNode;
  align?: 'left' | 'right';
  /** Numbers and machine values: right aligned, tabular figures. */
  numeric?: boolean;
  sortable?: boolean;
}

/** A table for comparing rows across the same columns. */
export function Table<Row>({ columns, rows, rowKey, caption, sort, onSort, empty }: { columns: Column<Row>[]; rows: Row[]; rowKey: (row: Row) => string; caption: string; sort?: { key: string; direction: 'asc' | 'desc' }; onSort?: (key: string) => void; empty?: ReactNode }) {
  return (
    <div className="overflow-x-auto rounded-xl border border-neutral-200 dark:border-neutral-800">
      <table className="w-full border-collapse text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead className="bg-neutral-900/[0.03] text-left text-xs text-neutral-500 dark:bg-white/[0.03] dark:text-neutral-400">
          <tr>
            {columns.map((column) => {
              const sorted = sort?.key === column.key ? sort.direction : undefined;
              const SortIcon = sorted === 'asc' ? ArrowUp : sorted === 'desc' ? ArrowDown : ArrowUpDown;
              return (
                <th key={column.key} scope="col" aria-sort={sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : undefined} className={cx('px-4 py-2.5 font-medium', (column.numeric || column.align === 'right') && 'text-right')}>
                  {column.sortable && onSort ? (
                    <button type="button" onClick={() => onSort(column.key)} className="inline-flex items-center gap-1 hover:text-neutral-900 dark:hover:text-neutral-100">
                      {column.header}
                      <SortIcon size={12} aria-hidden className={sorted ? '' : 'opacity-40'} />
                    </button>
                  ) : (
                    column.header
                  )}
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody className="divide-y divide-neutral-200 dark:divide-neutral-800">
          {rows.length === 0 && (
            <tr>
              <td colSpan={columns.length} className="px-4 py-8 text-center text-neutral-500">{empty}</td>
            </tr>
          )}
          {rows.map((row) => (
            <tr key={rowKey(row)} className="transition-colors hover:bg-neutral-900/[0.025] dark:hover:bg-white/[0.03]">
              {columns.map((column) => (
                <td key={column.key} className={cx('px-4 py-2.5 text-neutral-800 dark:text-neutral-100', (column.numeric || column.align === 'right') && 'text-right', column.numeric && 'font-mono tabular-nums')}>
                  {column.cell(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** Pairs of a name and its value (settings summary, task detail). */
export function DescriptionList({ items }: { items: Array<{ term: ReactNode; value: ReactNode; mono?: boolean }> }) {
  return (
    <dl className="grid grid-cols-[minmax(6rem,auto)_1fr] gap-x-6 gap-y-2.5 text-sm">
      {items.map((item, index) => (
        <div key={index} className="contents">
          <dt className="text-neutral-500 dark:text-neutral-400">{item.term}</dt>
          <dd className={cx('min-w-0 break-words text-neutral-900 dark:text-neutral-50', item.mono && 'font-mono text-[0.92em]')}>{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** Code, commands and output, with copying. */
export function CodeBlock({ children, label, copyLabel, copiedLabel }: { children: string; label?: string; copyLabel: string; copiedLabel: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <figure className="overflow-hidden rounded-xl border border-neutral-200 bg-neutral-900/[0.03] dark:border-neutral-800 dark:bg-black/30">
      {label !== undefined && (
        <figcaption className="flex items-center justify-between border-b border-neutral-200 px-3 py-1.5 text-xs text-neutral-500 dark:border-neutral-800 dark:text-neutral-400">
          <span className="font-mono">{label}</span>
          <button
            type="button"
            onClick={() => {
              void navigator.clipboard?.writeText(children);
              setCopied(true);
              window.setTimeout(() => setCopied(false), 1500);
            }}
            className={cx('inline-flex items-center gap-1 rounded px-1.5 py-0.5', interactive.hover)}
          >
            {copied ? <Check size={12} aria-hidden /> : <Copy size={12} aria-hidden />}
            {copied ? copiedLabel : copyLabel}
          </button>
        </figcaption>
      )}
      <pre className="overflow-x-auto px-3 py-2.5 font-mono text-xs leading-5 text-neutral-800 dark:text-neutral-100">{children}</pre>
    </figure>
  );
}

/** A number that matters, with its label and an optional note. */
export function Stat({ label, value, note }: { label: ReactNode; value: ReactNode; note?: ReactNode }) {
  return (
    <div>
      <p className="text-xs text-neutral-500 dark:text-neutral-400">{label}</p>
      <p className="mt-1 font-mono text-xl font-semibold tabular-nums text-neutral-900 dark:text-neutral-50">{value}</p>
      {note && <p className="mt-0.5 text-xs text-neutral-500 dark:text-neutral-400">{note}</p>}
    </div>
  );
}

export function Divider({ label }: { label?: ReactNode }) {
  if (!label) return <hr className="border-neutral-200 dark:border-neutral-800" />;
  return (
    <div role="separator" className="flex items-center gap-3 text-xs text-neutral-500 dark:text-neutral-400">
      <span className="h-px flex-1 bg-neutral-200 dark:bg-neutral-800" />
      {label}
      <span className="h-px flex-1 bg-neutral-200 dark:bg-neutral-800" />
    </div>
  );
}

/** Copies a value and says so for a moment; the label changes to confirm. */
export function CopyButton({ value, copyLabel, copiedLabel, size = 'sm' }: { value: string; copyLabel: string; copiedLabel: string; size?: 'sm' | 'md' }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      onClick={() => {
        void navigator.clipboard?.writeText(value);
        setCopied(true);
        window.setTimeout(() => setCopied(false), 1500);
      }}
      aria-label={copied ? copiedLabel : copyLabel}
      title={copied ? copiedLabel : copyLabel}
      className={cx('inline-flex items-center justify-center rounded-md text-neutral-500 transition-colors hover:text-neutral-900 dark:text-neutral-400 dark:hover:text-neutral-100', interactive.hover, size === 'sm' ? 'h-7 w-7' : 'h-9 w-9')}
    >
      {copied ? <Check size={14} aria-hidden className="text-emerald-500" /> : <Copy size={14} aria-hidden />}
    </button>
  );
}

/**
 * Sections that open and close one by one (details of a task, advanced
 * settings). Each header is a button; several may be open at once.
 */
export function Accordion({ items, defaultOpen = [] }: { items: Array<{ id: string; title: ReactNode; content: ReactNode; meta?: ReactNode }>; defaultOpen?: string[] }) {
  const [open, setOpen] = useState<Set<string>>(() => new Set(defaultOpen));
  const id = useId();
  return (
    <div className="divide-y divide-neutral-200 rounded-xl border border-neutral-200 dark:divide-neutral-800 dark:border-neutral-800">
      {items.map((item) => {
        const expanded = open.has(item.id);
        return (
          <div key={item.id}>
            <h3>
              <button
                type="button"
                id={`${id}-${item.id}-header`}
                aria-expanded={expanded}
                aria-controls={`${id}-${item.id}-panel`}
                onClick={() =>
                  setOpen((prev) => {
                    const next = new Set(prev);
                    if (next.has(item.id)) next.delete(item.id);
                    else next.add(item.id);
                    return next;
                  })
                }
                className={cx('flex w-full items-center gap-3 px-4 py-3 text-left text-sm font-medium text-neutral-900 dark:text-neutral-50', interactive.hover)}
              >
                <span className="min-w-0 flex-1">{item.title}</span>
                {item.meta && <span className="text-xs font-normal text-neutral-500 dark:text-neutral-400">{item.meta}</span>}
                <ChevronDown size={16} aria-hidden className={cx('shrink-0 text-neutral-400 transition-transform', expanded && 'rotate-180')} />
              </button>
            </h3>
            {expanded && (
              <div id={`${id}-${item.id}-panel`} role="region" aria-labelledby={`${id}-${item.id}-header`} className="px-4 pb-4 text-sm leading-6 text-neutral-600 dark:text-neutral-300">
                {item.content}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}