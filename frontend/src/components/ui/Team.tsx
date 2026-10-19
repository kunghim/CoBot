// CoBot's own pieces: members and their work. A member is its avatar and its
// name, shown together wherever a member is named. State is shown by
// the avatar (moving, wide eyes) and said in words for screen readers.
import { useId, useRef, type ReactNode } from 'react';
import { ArrowUp, Bot, ChevronLeft, ChevronRight, CircleCheck, CircleHelp, CircleX, FileDiff, FilePen, Globe, ListChecks, Loader2, MessageSquareText, ShieldCheck, Square, Terminal, Wrench, X } from 'lucide-react';
import { roleChip } from './roleChip';
import { Button, IconButton } from './Button';
import { cx, interactive } from './foundation';

/** A member's name as a badge in its role's color. */
export function RoleChip({ name, roleId, isLeader = false }: { name: ReactNode; roleId?: string; isLeader?: boolean }) {
  return <span className={cx('inline-flex max-w-[12rem] shrink-0 items-center truncate rounded-md px-1.5 py-px text-[11px] font-medium leading-4', roleChip(roleId, isLeader))}>{name}</span>;
}

/**
 * A member in a list: avatar, name, what it is doing, and the last thing
 * said in its conversation — one line, cut with an ellipsis, never wider
 * than the row. Its accessible name is the state and the name; the
 * status or time and the preview describe it.
 */
export function MemberRow({ avatar, name, status, statusLabel, preview, time, current = false, attention = false, onSelect }: { avatar: ReactNode; name: ReactNode; status?: ReactNode; statusLabel: string; preview?: ReactNode; time?: ReactNode; current?: boolean; attention?: boolean; onSelect: () => void }) {
  const id = useId();
  const meta = status ?? time;
  const describedBy = [meta && `${id}-meta`, preview && `${id}-preview`].filter(Boolean).join(' ') || undefined;
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={current || undefined}
      aria-labelledby={`${id}-state ${id}-name`}
      aria-describedby={describedBy}
      className={cx(
        'flex w-full min-w-0 items-center gap-2.5 rounded-lg px-2 text-left text-sm transition-colors',
        preview ? 'py-2' : 'py-1.5',
        current ? 'bg-white text-neutral-900 shadow-sm ring-1 ring-neutral-200 dark:bg-neutral-800 dark:text-neutral-50 dark:ring-neutral-700' : cx('text-neutral-600 dark:text-neutral-300', interactive.hover),
      )}
    >
      <span id={`${id}-state`} className="relative shrink-0" role="img" aria-label={statusLabel} title={statusLabel}>
        {avatar}
        {attention && <span className="absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full bg-amber-500 ring-2 ring-neutral-100 dark:ring-neutral-900" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-baseline gap-2">
          <span id={`${id}-name`} className={cx('min-w-0 flex-1 truncate', current ? 'font-semibold' : 'font-medium text-neutral-800 dark:text-neutral-100')}>{name}</span>
          {meta && <span id={`${id}-meta`} className="shrink-0 text-[11px] text-neutral-500 dark:text-neutral-400">{meta}</span>}
        </span>
        {preview && <span id={`${id}-preview`} className="mt-0.5 block truncate text-xs text-neutral-500 dark:text-neutral-400">{preview}</span>}
      </span>
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

/**
 * What the user and a member say to each other. A member's message carries
 * its avatar and name (plain text, no badge); the user's sits on the right.
 */
export function MessageBubble({ from, avatar, author, children, meta }: { from: 'user' | 'member'; avatar?: ReactNode; author?: ReactNode; children: ReactNode; meta?: ReactNode }) {
  const user = from === 'user';
  return (
    <div className={cx('flex gap-2.5', user ? 'justify-end' : 'justify-start')}>
      {!user && avatar && <div className="mt-0.5 shrink-0">{avatar}</div>}
      <div className="min-w-0 max-w-[85%] space-y-1">
        {author && !user && <div className="text-xs font-medium text-neutral-600 dark:text-neutral-300">{author}</div>}
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

/** A person named in a line: avatar and name, no badge. */
export interface PersonRef {
  avatar: ReactNode;
  name: ReactNode;
}

/**
 * A message passed between members: who it went to or came from, each with
 * its avatar. Clicking a name opens the exchange with that member.
 */
export function ExchangeLine({ label, people, onOpen }: { label: ReactNode; people: PersonRef[]; onOpen?: (index: number) => void }) {
  const person = (entry: PersonRef) => (
    <>
      {entry.avatar}
      <span className="font-medium text-neutral-700 dark:text-neutral-200">{entry.name}</span>
    </>
  );
  return (
    <div className="flex justify-center">
      <div className="flex flex-wrap items-center gap-1 text-xs text-neutral-500 dark:text-neutral-400">
        <span className="px-1">{label}</span>
        {people.map((entry, index) =>
          onOpen ? (
            <button key={index} type="button" onClick={() => onOpen(index)} className={cx('inline-flex items-center gap-1.5 rounded-full py-0.5 pl-0.5 pr-2', interactive.hover)}>
              {person(entry)}
            </button>
          ) : (
            <span key={index} className="inline-flex items-center gap-1.5 py-0.5 pl-0.5 pr-2">
              {person(entry)}
            </span>
          ),
        )}
      </div>
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

/**
 * The frame of a card that waits for the user above the composer: a quiet
 * raised surface, an icon and title, paging when there are several, the
 * content, and the answers as pill buttons on the right, the main one last.
 */
export function WaitingCard({ icon, title, position, onPrevious, onNext, previousLabel, nextLabel, onClose, closeLabel, children, actions }: { icon: ReactNode; title: ReactNode; position?: { index: number; total: number }; onPrevious?: () => void; onNext?: () => void; previousLabel: string; nextLabel: string; onClose?: () => void; closeLabel?: string; children: ReactNode; actions: ReactNode }) {
  return (
    <section className="animate-pop-in rounded-2xl border border-neutral-200 bg-white p-4 shadow-[0_1px_2px_rgb(23_30_27/0.06),0_12px_32px_-16px_rgb(23_30_27/0.25)] dark:border-neutral-700 dark:bg-neutral-800 dark:shadow-none">
      <header className="flex items-center gap-2 text-sm text-neutral-500 dark:text-neutral-400">
        <span className="flex shrink-0 items-center" aria-hidden>{icon}</span>
        <h3 className="min-w-0 flex-1 truncate">{title}</h3>
        {position && position.total > 1 && (
          <span className="flex items-center gap-0.5 text-xs tabular-nums">
            <IconButton icon={ChevronLeft} label={previousLabel} size="sm" shape="circle" disabled={position.index <= 1} onClick={onPrevious} />
            {position.index} / {position.total}
            <IconButton icon={ChevronRight} label={nextLabel} size="sm" shape="circle" disabled={position.index >= position.total} onClick={onNext} />
          </span>
        )}
        {onClose && <IconButton icon={X} label={closeLabel ?? ''} size="sm" shape="circle" onClick={onClose} />}
      </header>
      <div className="mt-3 space-y-3 text-sm text-neutral-900 dark:text-neutral-50">{children}</div>
      <div className="mt-4 flex flex-wrap items-center justify-end gap-2">{actions}</div>
    </section>
  );
}

/** A native tool request (run a command, change files) waiting for approval. */
export function ApprovalCard({ requester, action, command, reason, ...frame }: { requester: PersonRef; action: ReactNode; command?: ReactNode; reason?: ReactNode; title: ReactNode; position?: { index: number; total: number }; onPrevious?: () => void; onNext?: () => void; previousLabel: string; nextLabel: string; actions: ReactNode }) {
  return (
    <WaitingCard icon={<ShieldCheck size={16} />} {...frame}>
      <p className="flex flex-wrap items-center gap-1.5 font-semibold">
        <span className="inline-flex items-center gap-1.5">
          {requester.avatar}
          {requester.name}
        </span>
        <span className="font-normal text-neutral-600 dark:text-neutral-300">{action}</span>
      </p>
      {command && <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-neutral-900/[0.04] px-3 py-2 font-mono text-xs leading-5 text-neutral-800 dark:bg-black/30 dark:text-neutral-100">{command}</pre>}
      {reason && <p className="text-neutral-600 dark:text-neutral-300">{reason}</p>}
    </WaitingCard>
  );
}

/** A member's question to the user, answered in place. */
export function QuestionCard({ asker, question, answer, onAnswer, placeholder, options, ...frame }: { asker: PersonRef; question: ReactNode; answer: string; onAnswer: (value: string) => void; placeholder: string; options?: ReactNode; title: ReactNode; position?: { index: number; total: number }; onPrevious?: () => void; onNext?: () => void; previousLabel: string; nextLabel: string; onClose?: () => void; closeLabel?: string; actions: ReactNode }) {
  return (
    <WaitingCard icon={<CircleHelp size={16} />} {...frame}>
      <p className="flex items-center gap-1.5 text-xs text-neutral-500 dark:text-neutral-400">
        {asker.avatar}
        {asker.name}
      </p>
      <p className="font-semibold leading-6">{question}</p>
      {options}
      <input
        value={answer}
        onChange={(event) => onAnswer(event.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        className="h-11 w-full rounded-xl border border-neutral-200 bg-transparent px-3.5 text-sm outline-none transition-colors placeholder:text-neutral-400 focus:border-signal-500 focus:ring-2 focus:ring-signal-500/25 dark:border-neutral-600"
      />
    </WaitingCard>
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
          <IconButton icon={(props) => <Square {...props} size={11} fill="currentColor" />} label={stopLabel} variant="primary" size="sm" shape="circle" onClick={onStop} />
        ) : (
          <IconButton icon={ArrowUp} label={sendLabel} variant="primary" size="sm" shape="circle" disabled={disabled || empty} onClick={onSend} className="disabled:opacity-30" />
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
