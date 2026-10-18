// CoBot's own pieces: members and their work. A member is its avatar and its
// name; the role's color carries through its name badge. State is shown by
// the avatar (moving, wide eyes) and said in words for screen readers.
import { useRef, type ReactNode } from 'react';
import { ArrowUp, Bot, ChevronLeft, ChevronRight, CircleCheck, CircleX, FileDiff, FilePen, Globe, ListChecks, Loader2, MessageSquareText, Square, Terminal, Wrench } from 'lucide-react';
import { roleChip } from './roleChip';
import { Button, IconButton } from './Button';
import { cx, interactive, toneText } from './foundation';

/** A member's name as a badge in its role's color. */
export function RoleChip({ name, roleId, isLeader = false }: { name: ReactNode; roleId?: string; isLeader?: boolean }) {
  return <span className={cx('inline-flex max-w-[12rem] shrink-0 items-center truncate rounded-md px-1.5 py-px text-[11px] font-medium leading-4', roleChip(roleId, isLeader))}>{name}</span>;
}

/** A member in a list: avatar, name, and what it is doing. */
export function MemberRow({ avatar, name, status, statusLabel, current = false, attention = false, onSelect }: { avatar: ReactNode; name: ReactNode; status?: ReactNode; statusLabel: string; current?: boolean; attention?: boolean; onSelect: () => void }) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={current || undefined}
      className={cx(
        'flex w-full min-w-0 items-center gap-2.5 rounded-lg px-2 py-1.5 text-left text-sm transition-colors',
        current ? 'bg-white text-neutral-900 shadow-sm ring-1 ring-neutral-200 dark:bg-neutral-800 dark:text-neutral-50 dark:ring-neutral-700' : cx('text-neutral-600 dark:text-neutral-300', interactive.hover),
      )}
    >
      <span className="relative" role="img" aria-label={statusLabel} title={statusLabel}>
        {avatar}
        {attention && <span className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full bg-amber-500 ring-2 ring-neutral-100 dark:ring-neutral-900" />}
      </span>
      <span className={cx('min-w-0 flex-1 truncate', current && 'font-medium')}>{name}</span>
      {status && <span className="shrink-0 text-[11px] text-neutral-500 dark:text-neutral-400">{status}</span>}
    </button>
  );
}

/** Several members at a glance, overlapping. */
export function AvatarStack({ avatars, more }: { avatars: ReactNode[]; more?: string }) {
  return (
    <span className="flex items-center">
      {avatars.map((avatar, index) => (
        <span key={index} className="-ml-1.5 first:ml-0 rounded-[30%] bg-white ring-2 ring-white dark:bg-neutral-900 dark:ring-neutral-900">
          {avatar}
        </span>
      ))}
      {more && <span className="ml-1.5 text-xs text-neutral-500">{more}</span>}
    </span>
  );
}

/** What the user and a member say to each other. */
export function MessageBubble({ from, author, children, meta }: { from: 'user' | 'member'; author?: ReactNode; children: ReactNode; meta?: ReactNode }) {
  const user = from === 'user';
  return (
    <div className={cx('flex', user ? 'justify-end' : 'justify-start')}>
      <div className="max-w-[85%] space-y-1">
        {author && !user && <div>{author}</div>}
        <div
          className={cx(
            'break-words text-sm leading-6',
            user ? 'whitespace-pre-wrap rounded-2xl rounded-br-md bg-neutral-900 px-3.5 py-2 text-white dark:bg-neutral-100 dark:text-neutral-900' : 'rounded-2xl rounded-tl-md bg-white px-4 py-2.5 text-neutral-900 ring-1 ring-neutral-200 dark:bg-neutral-900 dark:text-neutral-100 dark:ring-neutral-800',
          )}
        >
          {children}
        </div>
        {meta && <div className={cx('flex items-center gap-2 text-[11px] text-neutral-500 dark:text-neutral-400', user ? 'justify-end' : 'justify-start')}>{meta}</div>}
      </div>
    </div>
  );
}

/** A note from CoBot itself in a conversation, centered and quiet. */
export function SystemNote({ children }: { children: ReactNode }) {
  return (
    <div className="flex justify-center">
      <span className="max-w-[85%] whitespace-pre-wrap text-center text-xs text-neutral-500 dark:text-neutral-400">{children}</span>
    </div>
  );
}

/** A message passed between members: who it went to or came from. Opens the exchange. */
export function ExchangeLine({ label, chips, onOpen }: { label: ReactNode; chips: ReactNode; onOpen?: () => void }) {
  const content = (
    <>
      <span>{label}</span>
      {chips}
    </>
  );
  return (
    <div className="flex justify-center">
      {onOpen ? (
        <button type="button" onClick={onOpen} className={cx('flex flex-wrap items-center gap-1.5 rounded-full px-2.5 py-1 text-xs text-neutral-500 hover:text-neutral-800 dark:text-neutral-400 dark:hover:text-neutral-100', interactive.hover)}>
          {content}
        </button>
      ) : (
        <div className="flex flex-wrap items-center gap-1.5 px-2.5 py-1 text-xs text-neutral-500 dark:text-neutral-400">{content}</div>
      )}
    </div>
  );
}

export type StepKind = 'reasoning' | 'command' | 'file_change' | 'tool' | 'web_search' | 'plan' | 'message' | 'agent' | 'diff';
export type StepStatus = 'in_progress' | 'completed' | 'failed' | 'declined' | 'interrupted';

const stepIcon: Record<StepKind, (props: { size?: number; 'aria-hidden'?: boolean }) => ReactNode> = {
  reasoning: MessageSquareText,
  command: Terminal,
  file_change: FilePen,
  tool: Wrench,
  web_search: Globe,
  plan: ListChecks,
  message: MessageSquareText,
  agent: Bot,
  diff: FileDiff,
};

/** One step of a member's turn: what it did, how it went, how long it took. */
export function ProcessStep({ kind, status, title, detail, duration, badge }: { kind: StepKind; status: StepStatus; title: ReactNode; detail?: ReactNode; duration?: string; badge?: ReactNode }) {
  const Icon = stepIcon[kind];
  const statusIcon =
    status === 'in_progress' ? <Loader2 size={13} className="animate-spin text-signal-500" aria-hidden /> : status === 'completed' ? <CircleCheck size={13} className="text-emerald-500" aria-hidden /> : <CircleX size={13} className={status === 'failed' ? 'text-red-500' : 'text-neutral-400'} aria-hidden />;
  return (
    <details className="group rounded-lg">
      <summary className={cx('flex cursor-pointer list-none items-center gap-2 rounded-lg px-2 py-1.5 text-xs [&::-webkit-details-marker]:hidden', interactive.hover)}>
        <Icon size={14} aria-hidden />
        <span className="min-w-0 flex-1 truncate text-neutral-700 dark:text-neutral-200">{title}</span>
        {badge}
        {duration && <span className="font-mono tabular-nums text-neutral-400">{duration}</span>}
        {statusIcon}
      </summary>
      {detail && <div className="ml-6 mt-1 border-l border-neutral-200 pl-3 text-xs text-neutral-600 dark:border-neutral-800 dark:text-neutral-300">{detail}</div>}
    </details>
  );
}

/** A request waiting for the user, one at a time with paging. */
export function ApprovalCard({ title, position, onPrevious, onNext, previousLabel, nextLabel, requester, body, reason, actions, tone = 'warning' }: { title: ReactNode; position?: { index: number; total: number }; onPrevious?: () => void; onNext?: () => void; previousLabel: string; nextLabel: string; requester?: ReactNode; body?: ReactNode; reason?: ReactNode; actions: ReactNode; tone?: 'warning' | 'info' }) {
  const frame = tone === 'warning' ? 'border-amber-300 bg-amber-50/70 dark:border-amber-800 dark:bg-amber-950/40' : 'border-sky-300 bg-sky-50/70 dark:border-sky-800 dark:bg-sky-950/40';
  return (
    <section className={cx('rounded-xl border', frame)} aria-label={typeof title === 'string' ? title : undefined}>
      <header className="flex items-center gap-2 border-b border-inherit px-3 py-1.5">
        <h3 className={cx('text-xs font-semibold', toneText[tone])}>{title}</h3>
        {requester}
        {position && position.total > 1 && (
          <div className="ml-auto flex items-center gap-0.5 text-xs text-neutral-600 dark:text-neutral-300">
            <IconButton icon={ChevronLeft} label={previousLabel} size="sm" disabled={position.index <= 1} onClick={onPrevious} />
            <span className="tabular-nums">{position.index} / {position.total}</span>
            <IconButton icon={ChevronRight} label={nextLabel} size="sm" disabled={position.index >= position.total} onClick={onNext} />
          </div>
        )}
      </header>
      <div className="space-y-2 px-3 py-2.5 text-sm">
        {body}
        {reason && <p className="text-neutral-700 dark:text-neutral-200">{reason}</p>}
        <div className="flex flex-wrap gap-2 pt-1">{actions}</div>
      </div>
    </section>
  );
}

/** Where the user writes to a member: the text, its options, send or stop. */
export function Composer({ value, onChange, onSend, placeholder, running = false, onStop, sendLabel, stopLabel, options, disabled = false }: { value: string; onChange: (value: string) => void; onSend: () => void; placeholder: string; running?: boolean; onStop?: () => void; sendLabel: string; stopLabel: string; options?: ReactNode; disabled?: boolean }) {
  const area = useRef<HTMLTextAreaElement>(null);
  const empty = value.trim() === '';
  return (
    <div className="rounded-2xl border border-neutral-200 bg-white shadow-[0_1px_2px_rgb(23_30_27/0.06),0_8px_24px_-12px_rgb(23_30_27/0.18)] transition-colors focus-within:border-neutral-400 dark:border-neutral-700 dark:bg-neutral-900 dark:shadow-none dark:focus-within:border-neutral-500">
      <textarea
        ref={area}
        rows={1}
        value={value}
        disabled={disabled}
        aria-label={placeholder}
        placeholder={placeholder}
        onChange={(event) => {
          onChange(event.target.value);
          const element = event.target;
          element.style.height = 'auto';
          element.style.height = `${Math.min(element.scrollHeight, 240)}px`;
        }}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
          event.preventDefault();
          if (!empty) onSend();
        }}
        className="block max-h-60 min-h-[44px] w-full resize-none bg-transparent px-4 pb-1 pt-3 text-sm leading-6 placeholder:text-neutral-400 focus:outline-none disabled:opacity-60"
      />
      <div className="flex items-center justify-between gap-2 px-2 pb-2">
        <div className="flex min-w-0 items-center gap-1">{options}</div>
        {running && empty ? (
          <IconButton icon={(props) => <Square {...props} size={11} fill="currentColor" />} label={stopLabel} variant="primary" size="sm" onClick={onStop} className="rounded-full" />
        ) : (
          <IconButton icon={ArrowUp} label={sendLabel} variant="primary" size="sm" disabled={disabled || empty} onClick={onSend} className="rounded-full disabled:opacity-30" />
        )}
      </div>
    </div>
  );
}

/** A compact option inside the composer (permission mode, reasoning effort). */
export function ComposerOption({ icon: Icon, children, onClick }: { icon: (props: { size?: number; 'aria-hidden'?: boolean }) => ReactNode; children: ReactNode; onClick?: () => void }) {
  return (
    <Button variant="ghost" size="sm" icon={Icon} onClick={onClick} className="text-neutral-500 dark:text-neutral-400">
      {children}
    </Button>
  );
}
