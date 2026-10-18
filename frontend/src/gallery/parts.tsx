// Building blocks of the gallery page itself (not part of the product).
import type { ReactNode } from 'react';
import { cx } from '../components/ui';

/** One component's entry: what it is for, when not to use it, its demos. */
export function Doc({ id, name, file, use, avoid, a11y, children }: { id: string; name: string; file: string; use: ReactNode; avoid?: ReactNode; a11y?: ReactNode; children: ReactNode }) {
  return (
    <article id={id} className="scroll-mt-20 border-t border-neutral-200 pt-8 dark:border-neutral-800">
      <header className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h3 className="text-lg font-semibold tracking-tight">{name}</h3>
        <code className="font-mono text-xs text-neutral-500 dark:text-neutral-400">{file}</code>
      </header>
      <dl className="mt-3 grid gap-x-6 gap-y-1.5 text-sm leading-6 sm:grid-cols-[5.5rem_1fr]">
        <dt className="font-medium text-neutral-500 dark:text-neutral-400">用于</dt>
        <dd className="text-neutral-800 dark:text-neutral-100">{use}</dd>
        {avoid && (
          <>
            <dt className="font-medium text-neutral-500 dark:text-neutral-400">不要用于</dt>
            <dd className="text-neutral-800 dark:text-neutral-100">{avoid}</dd>
          </>
        )}
        {a11y && (
          <>
            <dt className="font-medium text-neutral-500 dark:text-neutral-400">无障碍</dt>
            <dd className="text-neutral-800 dark:text-neutral-100">{a11y}</dd>
          </>
        )}
      </dl>
      <div className="mt-5 space-y-4">{children}</div>
    </article>
  );
}

/** A labeled live example. */
export function Demo({ title, children, className, surface = 'page' }: { title?: ReactNode; children: ReactNode; className?: string; surface?: 'page' | 'raised' | 'sunken' }) {
  return (
    <figure className="m-0">
      {title && <figcaption className="mb-2 text-xs font-medium text-neutral-500 dark:text-neutral-400">{title}</figcaption>}
      <div
        className={cx(
          'rounded-xl border border-dashed border-neutral-300 p-5 dark:border-neutral-700',
          surface === 'raised' && 'bg-white dark:bg-neutral-900',
          surface === 'sunken' && 'bg-neutral-100 dark:bg-neutral-900',
          className,
        )}
      >
        {children}
      </div>
    </figure>
  );
}

/** A row of labeled cells, for state matrices and variants. */
export function Matrix({ items }: { items: Array<{ label: ReactNode; node: ReactNode }> }) {
  return (
    <div className="flex flex-wrap items-end gap-x-6 gap-y-4">
      {items.map((item, index) => (
        <div key={index} className="flex flex-col items-start gap-2">
          {item.node}
          <span className="text-[11px] text-neutral-500 dark:text-neutral-400">{item.label}</span>
        </div>
      ))}
    </div>
  );
}

/** A right / wrong pair. */
export function DoDont({ good, bad, goodNote, badNote }: { good: ReactNode; bad: ReactNode; goodNote: ReactNode; badNote: ReactNode }) {
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <figure className="m-0 overflow-hidden rounded-xl border-2 border-emerald-500/60">
        <div className="p-5">{good}</div>
        <figcaption className="border-t border-emerald-500/30 bg-emerald-500/[0.07] px-4 py-2 text-xs leading-5 text-emerald-800 dark:text-emerald-200">
          <b>这样做：</b>
          {goodNote}
        </figcaption>
      </figure>
      <figure className="m-0 overflow-hidden rounded-xl border-2 border-red-500/60">
        <div className="p-5">{bad}</div>
        <figcaption className="border-t border-red-500/30 bg-red-500/[0.07] px-4 py-2 text-xs leading-5 text-red-800 dark:text-red-200">
          <b>不要这样：</b>
          {badNote}
        </figcaption>
      </figure>
    </div>
  );
}

/** A heading for a group of entries in the gallery. */
export function Chapter({ id, title, intro, children }: { id: string; title: string; intro?: ReactNode; children: ReactNode }) {
  return (
    <section id={id} className="scroll-mt-20 space-y-8">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">{title}</h2>
        {intro && <p className="mt-2 max-w-3xl text-sm leading-6 text-neutral-600 dark:text-neutral-300">{intro}</p>}
      </div>
      {children}
    </section>
  );
}

/** A plain list of rules, read by people and AI alike. */
export function Rules({ items }: { items: ReactNode[] }) {
  return (
    <ul className="list-disc space-y-1.5 pl-5 text-sm leading-6 text-neutral-800 marker:text-neutral-400 dark:text-neutral-100">
      {items.map((item, index) => (
        <li key={index}>{item}</li>
      ))}
    </ul>
  );
}
