// A card that floats in a corner and takes the place of the content beside
// it. It is as tall as its content and the caller caps it where it must
// stop; past that its content scrolls. Escape closes it from anywhere it
// holds focus; the opener takes the focus back through onClose in the
// caller.
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
      // One width for every tab of the work panel: sized to what its tab
      // row needs, so switching tabs never reshapes the card — the content
      // wraps instead.
      className={cx('flex min-h-0 w-80 max-w-[38vw] flex-col overflow-hidden rounded-2xl bg-side shadow-sheet ring-1 ring-neutral-900/[0.06] dark:ring-white/[0.08]', className)}
    >
      <div className="relative flex min-h-0 flex-1 flex-col">
        {/* The close sits on the content's own first line, so a header row of
            its own cannot add an empty line above the card's tabs. The inner
            wrapper owns the positioning context: the card itself takes
            whatever placement its caller gives it. */}
        <IconButton size="sm" icon={Icons.close} label={closeLabel} onClick={onClose} className="absolute right-2 top-2 z-10" />
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-1 pt-1 pb-2 [overflow-wrap:break-word]">{children}</div>
      </div>
    </div>
  );
}
