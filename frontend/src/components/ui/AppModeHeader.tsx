import type { ReactNode } from 'react';
import { BrandMark } from './BrandMark';
import { cx } from './foundation';

/** The four compact slots in the title bar; the content columns start below it. */
export function AppModeHeader({ brandLabel, children, toggle, compact = false }: { brandLabel: string; children: ReactNode; toggle?: ReactNode; compact?: boolean }) {
  return <div data-testid="app-mode-header" className="flex h-10 shrink-0 items-center text-neutral-900 dark:text-neutral-100">
    <div data-testid="chrome-logo" className="flex w-14 shrink-0 items-center justify-center"><BrandMark size={26} /></div>
    <div data-testid="chrome-wordmark" className={cx('w-[clamp(3.5rem,19vw,8rem)] shrink-0 truncate px-2 text-sm font-semibold tracking-tight', compact && 'max-sm:hidden')}>{brandLabel}</div>
    <div className="flex shrink-0 items-center">{children}</div>
    <div className="ml-2 flex shrink-0 items-center">{toggle}</div>
  </div>;
}
