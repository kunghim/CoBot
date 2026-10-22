// A card that floats in a corner and takes the place of the content beside
// it. Escape closes it from anywhere it holds focus; the opener takes the
// focus back through onClose in the caller.
import { useEffect, useRef, type ReactNode } from 'react';
import { IconButton } from './Button';
import { Icons } from './icons';
import { cx } from './foundation';

export interface FloatingCardProps {
  /** What the card is, read by screen readers on its region. */
  label: string;
  closeLabel: string;
  onClose: () => void;
  children: ReactNode;
  className?: string;
}

export function FloatingCard({ label, closeLabel, onClose, children, className }: FloatingCardProps) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.stopPropagation();
        onClose();
      }
    };
    // The whole conversation listens while the card is up: Esc works from
    // the messages behind it too.
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [onClose]);

  return (
    <div
      ref={ref}
      role="region"
      aria-label={label}
      className={cx('flex min-h-0 w-96 max-w-[38vw] shrink-0 flex-col overflow-hidden rounded-2xl bg-side shadow-sheet ring-1 ring-neutral-900/[0.06] dark:ring-white/[0.08]', className)}
    >
      <div className="flex items-center justify-end px-2 pt-2">
        <IconButton size="sm" icon={Icons.close} label={closeLabel} onClick={onClose} />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-1 pb-2">{children}</div>
    </div>
  );
}
