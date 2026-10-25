import type { ReactNode } from 'react';
import { cx } from './foundation';

/** Match the rail, visible sidebar and conversation columns below the title bar. */
export function AppModeHeader({ children, leading, toggle, listVisible = true }: { children: ReactNode; leading?: ReactNode; toggle?: ReactNode; listVisible?: boolean }) {
  return <div data-testid="app-mode-header" className="flex h-10 shrink-0 items-center text-neutral-900 dark:text-neutral-100">
    <div className="w-14 shrink-0" aria-hidden />
    {listVisible && <div className={cx('flex w-[min(16rem,40vw)] shrink-0 items-center gap-2 px-2', leading ? 'justify-between' : 'justify-end')}>{leading}{children}</div>}
    <div className="flex shrink-0 items-center">{toggle}</div>
  </div>;
}
