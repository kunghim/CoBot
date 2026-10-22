// The line between two areas: drag it with the pointer or move it with the
// arrow keys. It reports the width of the area it guards, so the caller
// remembers what the user chose.
import { useRef, type KeyboardEvent, type PointerEvent } from 'react';
import { cx } from './foundation';

export interface SplitHandleProps {
  /** The guarded area's width in pixels. */
  value: number;
  min: number;
  max: number;
  /** Pixels one arrow key or one drag tick moves the line. */
  step?: number;
  label: string;
  onChange: (width: number) => void;
  /** Called once when a drag ends, to settle the width. */
  onCommit?: (width: number) => void;
}

export function SplitHandle({ value, min, max, step = 16, label, onChange, onCommit }: SplitHandleProps) {
  const dragging = useRef(false);

  const widthFrom = (clientX: number) => Math.min(max, Math.max(min, Math.round(window.innerWidth - clientX)));

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    dragging.current = true;
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!dragging.current) return;
    onChange(widthFrom(event.clientX));
  };
  const endDrag = (event: PointerEvent<HTMLDivElement>) => {
    if (!dragging.current) return;
    dragging.current = false;
    event.currentTarget.releasePointerCapture(event.pointerId);
    onCommit?.(value);
  };
  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    // The line sits left of its area: ArrowLeft widens, ArrowRight narrows.
    const large = event.shiftKey ? 4 : 1;
    if (event.key === 'ArrowLeft') onChange(Math.min(max, value + step * large));
    else if (event.key === 'ArrowRight') onChange(Math.max(min, value - step * large));
    else return;
    event.preventDefault();
  };

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      onKeyDown={onKeyDown}
      className={cx(
        'w-1 shrink-0 cursor-col-resize touch-none bg-transparent transition-colors',
        'hover:bg-neutral-900/10 focus-visible:bg-neutral-900/20 dark:hover:bg-white/10 dark:focus-visible:bg-white/20',
      )}
    />
  );
}
