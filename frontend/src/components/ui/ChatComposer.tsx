import type { ReactNode } from 'react';
import { ArrowUp, Loader2, Square } from 'lucide-react';
import { IconButton } from './Button';
import { Textarea } from './Input';

/** Chat keeps its own layout; the shared textarea owns growing and field semantics. */
export function ChatComposer({ value, onChange, onSend, placeholder, sendLabel, stopLabel, options, canSend = true, sending = false, running = false, stopping = false, onStop }: {
  value: string; onChange: (value: string) => void; onSend: () => void;
  placeholder: string; sendLabel: string; stopLabel: string; options?: ReactNode;
  canSend?: boolean; sending?: boolean; running?: boolean; stopping?: boolean; onStop?: () => void;
}) {
  const blocked = !value.trim() || !canSend || sending || running;
  return <div data-testid="chat-composer" className="rounded-3xl border border-neutral-900/10 bg-card p-3 shadow-sm transition-colors focus-within:border-neutral-900/25 dark:border-white/10 dark:focus-within:border-white/25 sm:p-4">
    <Textarea variant="plain" autoGrowMax={240} rows={2} value={value} aria-label={placeholder} placeholder={placeholder} className="px-1 pb-2 pt-0"
      onChange={(event) => onChange(event.target.value)} onKeyDown={(event) => {
        if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing || event.keyCode === 229) return;
        event.preventDefault();
        if (!blocked) onSend();
      }} />
    <div className="mt-2 flex items-end justify-between gap-2">
      <div className="flex min-w-0 flex-wrap items-center gap-1">{options}</div>
      {running ? <IconButton icon={stopping ? (props) => <Loader2 {...props} className="animate-spin" /> : (props) => <Square {...props} size={11} fill="currentColor" />} label={stopLabel} variant="primary" shape="circle" size="sm" disabled={stopping} onClick={onStop} />
        : <IconButton icon={ArrowUp} label={sendLabel} variant="primary" shape="circle" size="sm" disabled={blocked} onClick={onSend} className="disabled:opacity-30" />}
    </div>
  </div>;
}
