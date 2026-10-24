import { useLayoutEffect, useRef, type ReactNode } from 'react';
import { ArrowUp, Loader2, Square } from 'lucide-react';
import { IconButton } from './Button';

/**
 * The chat's input: the same size and frame as the team's (Composer), one
 * line that grows with the text. What differs is the logic of a chat: while a
 * turn runs the draft stays editable and the stop button is always there, and
 * the keyboard of an input method (keyCode 229) never sends.
 */
export function ChatComposer({ value, onChange, onSend, placeholder, sendLabel, stopLabel, options, canSend = true, sending = false, running = false, stopping = false, onStop }: {
  value: string; onChange: (value: string) => void; onSend: () => void;
  placeholder: string; sendLabel: string; stopLabel: string; options?: ReactNode;
  canSend?: boolean; sending?: boolean; running?: boolean; stopping?: boolean; onStop?: () => void;
}) {
  const area = useRef<HTMLTextAreaElement>(null);
  const blocked = !value.trim() || !canSend || sending || running;
  useLayoutEffect(() => {
    const element = area.current;
    if (!element) return;
    element.style.height = 'auto';
    element.style.height = `${element.scrollHeight}px`;
  }, [value]);
  return (
    <div data-testid="chat-composer" className="rounded-2xl border border-neutral-900/[0.12] bg-card shadow-raised transition-colors focus-within:border-neutral-900/25 dark:border-white/[0.12] dark:focus-within:border-white/25">
      <textarea
        ref={area}
        rows={1}
        value={value}
        aria-label={placeholder}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing || event.keyCode === 229) return;
          event.preventDefault();
          if (!blocked) onSend();
        }}
        className="block max-h-60 min-h-[44px] w-full resize-none bg-transparent px-4 pb-1 pt-3 text-sm leading-6 placeholder:text-neutral-400 focus:outline-none"
      />
      <div className="flex items-center justify-between gap-2 px-2 pb-2">
        <div className="flex min-w-0 items-center gap-1">{options}</div>
        {running ? (
          <IconButton
            icon={stopping ? (props) => <Loader2 {...props} size={14} className="animate-spin" /> : (props) => <Square {...props} size={11} fill="currentColor" />}
            label={stopLabel}
            variant="primary"
            size="sm"
            shape="circle"
            disabled={stopping}
            onClick={onStop}
          />
        ) : (
          <IconButton icon={ArrowUp} label={sendLabel} variant="primary" size="sm" shape="circle" disabled={blocked} onClick={onSend} className="disabled:opacity-30" />
        )}
      </div>
    </div>
  );
}
