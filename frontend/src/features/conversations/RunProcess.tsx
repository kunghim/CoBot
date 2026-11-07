// RunProcess shows one turn the way Codex does, in the order it happened:
// what the member said stays in view between stretches of work, and each
// stretch of consecutive same-kind steps folds to a line ("运行了 3 条命令")
// that opens into its steps. While it runs, finished stretches fold away and
// only the one in progress stays open, a live status line ("正在执行命令 ·
// 12秒") sits at the very bottom under the newest content, and the reply
// streams in; once finished, the whole process folds to one "用时 1分23秒"
// line above the reply and opens on click. A failed turn stays open under
// its own line, an interrupted one is marked. A team's turns and a chat's
// both use it (see Scope).
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Loader2, Minus, X } from 'lucide-react';
import { Badge, Button, cx, Icons, interactive } from '../../components/ui';
import type { RunStep, RunWithSteps } from '../../api/types';
import type { LiveRun } from '../../api/live';
import { mergeSteps } from '../../api/live';
import Markdown from '../../components/content/Markdown';
import { stripToolCallText } from './toolCallText';
import { stepKindOf, type Scope } from './stepKinds';
import { describeStep, displayCommand, durationPhrase, elapsedSeconds, runPhase, turnBlocks, visibleSteps, workNeedsAttention, workSummary, type Phrase } from './steps';
import { openModelPicker } from './modelPicker';

export type { Scope };

/** What a turn needs of a run: its place in time, its state and its steps. */
export type ProcessRun = Pick<RunWithSteps, 'id' | 'status' | 'startedAt' | 'endedAt' | 'steps' | 'botId'>;

function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return now;
}

/** Whether a turn has anything to show above its reply: what is being done, what went wrong, or what was done. */
export function runHasContent(run: ProcessRun, active: boolean, replyText: string): boolean {
  if (active || run.status === 'failed' || run.status === 'interrupted') return true;
  return visibleSteps(mergeSteps(run.steps, undefined), undefined, replyText).length > 0;
}

export default function RunProcess({
  run,
  active,
  live,
  scope,
  nameOf,
  replyText = '',
  onRetry,
  retrying = false,
}: {
  run: ProcessRun;
  active: boolean;
  live: LiveRun | undefined;
  scope: Scope;
  nameOf: (botId: string) => string;
  /** The turn's reply, shown right under it: what it holds is not said twice. */
  replyText?: string;
  /** Given for the member's latest failed turn while it is idle: run the work again. */
  onRetry?: () => void;
  retrying?: boolean;
}) {
  const { t } = useTranslation();
  const retry = onRetry && run.status === 'failed' && (
    <Button size="sm" variant="secondary" loading={retrying} onClick={onRetry} icon={Icons.retry}>
      {t('conversation.process.retry')}
    </Button>
  );
  const say = (phrase: Phrase) => t(phrase.key, phrase.params);
  const now = useNow(active);
  // null: follow the default; true or false: the user opened or folded all the work.
  const [open, setOpen] = useState<boolean | null>(null);
  const steps = mergeSteps(run.steps, active ? live : undefined);
  const shown = visibleSteps(steps, active ? live : undefined, replyText);
  const blocks = turnBlocks(shown);
  // The work of a running or failed turn stays open so what is going on, or what went wrong, is seen without a click.
  const expanded = open ?? (active || run.status === 'failed');

  const endedAt = run.endedAt ? Date.parse(run.endedAt) : NaN;
  const seconds = elapsedSeconds(run.startedAt, active || Number.isNaN(endedAt) ? now : endedAt);
  const duration = say(durationPhrase(seconds));
  // The steps live on screen while the turn runs, while something is wrong,
  // and when the user opens a finished turn's record; a finished turn folds
  // them behind its duration line.
  const showProcess = active || run.status === 'failed' || run.status === 'interrupted' || expanded;
  // The stretch the member is working in now is the last one on the turn.
  let lastWorkIndex = -1;
  blocks.forEach((block, index) => {
    if (block.kind === 'work') lastWorkIndex = index;
  });
  const phaseLine = active && (
    <p role="status" className="-ml-1 flex items-center gap-1.5 px-1.5 text-xs text-neutral-500 dark:text-neutral-400">
      <Loader2 size={13} className="animate-spin text-signal-500" aria-hidden="true" />
      <span>
        {say(runPhase(run, steps, live, seconds))} · {duration}
      </span>
    </p>
  );

  if (!active && shown.length === 0) {
    if (run.status === 'failed' || run.status === 'interrupted') {
      return (
        <div className="flex items-center gap-3">
          <p className="text-xs text-neutral-500 dark:text-neutral-400">{t(`conversation.process.${run.status}`)}</p>
          {retry}
        </div>
      );
    }
    return null;
  }

  const summary = t(
    run.status === 'failed' ? 'conversation.process.failedAfter' : run.status === 'interrupted' ? 'conversation.process.interruptedAfter' : 'conversation.process.done',
    { duration },
  );

  return (
    <div className="min-w-0 space-y-2" data-testid="run-process">
      {!active && (
        <button
          type="button"
          onClick={() => setOpen(!expanded)}
          aria-expanded={expanded}
          className={cx('-ml-1.5 flex items-center gap-1.5 rounded-lg px-1.5 py-0.5 text-xs text-neutral-500 transition-colors hover:text-neutral-800 dark:text-neutral-400 dark:hover:text-neutral-200', interactive.hover)}
        >
          {run.status !== 'failed' && run.status !== 'interrupted' && (expanded ? <Icons.expand size={13} aria-hidden /> : <Icons.next size={13} aria-hidden />)}
          <span>{summary}</span>
        </button>
      )}
      {showProcess &&
        blocks.map((block, index) =>
          block.kind === 'text' ? (
            <StepRow key={block.step.id} step={block.step} botId={run.botId} running={active && block.step.status === 'in_progress'} live={active ? live : undefined} scope={scope} nameOf={nameOf} />
          ) : (
            <WorkGroup
              key={block.id}
              steps={block.steps}
              botId={run.botId}
              // While the turn runs, the finished stretches fold away and only
              // the one the member is working in now stays open; something
              // that went wrong always opens, as does a failed turn's work.
              expanded={expanded || (active && index === lastWorkIndex) || workNeedsAttention(block.steps)}
              active={active}
              live={active ? live : undefined}
              scope={scope}
              nameOf={nameOf}
              say={say}
            />
          ),
        )}
      {retry}
      {active && live?.reply && (
        <div className="text-sm leading-7 text-neutral-900 dark:text-neutral-100" data-testid="streaming-reply">
          <Markdown text={stripToolCallText(live.reply)} teamId={scope.teamId} chatId={scope.chatId} />
        </div>
      )}
      {phaseLine}
    </div>
  );
}

/** A stretch of work between two things the member said: a line saying what it
 * held, which opens into the steps. While the turn runs only the current
 * stretch is open by default; the user can open any of them. */
function WorkGroup({ steps, botId, expanded, active, live, scope, nameOf, say }: { steps: RunStep[]; botId: string; expanded: boolean; active: boolean; live: LiveRun | undefined; scope: Scope; nameOf: (botId: string) => string; say: (phrase: Phrase) => string }) {
  const [own, setOwn] = useState<boolean | null>(null);
  const open = own ?? expanded;
  const summary = workSummary(steps).map(say).join('、');
  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOwn(!open)}
        className={cx('-ml-1.5 flex items-center gap-1.5 rounded-lg px-1.5 py-0.5 text-xs text-neutral-500 transition-colors hover:text-neutral-800 dark:text-neutral-400 dark:hover:text-neutral-200', interactive.hover)}
      >
        {open ? <Icons.expand size={13} aria-hidden /> : <Icons.next size={13} aria-hidden />}
        <span>{summary}</span>
      </button>
      {open && (
        <ol className="ml-1.5 mt-1 space-y-1 border-l border-neutral-200 pl-3 dark:border-neutral-700">
          {steps.map((step) => (
            <li key={step.id}>
              <StepRow step={step} botId={botId} running={active && step.status === 'in_progress'} live={live} scope={scope} nameOf={nameOf} />
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}

function StatusMark({ status, running }: { status: string; running: boolean }) {
  if (running) return <Loader2 size={11} className="animate-spin text-neutral-400" aria-hidden="true" />;
  if (status === 'failed' || status === 'declined') return <X size={11} className="text-red-500" aria-hidden="true" />;
  if (status === 'interrupted') return <Minus size={11} className="text-neutral-400" aria-hidden="true" />;
  return <Check size={11} className="text-neutral-400" aria-hidden="true" />;
}

function StepRow({ step, botId, running, live, scope, nameOf }: { step: RunStep; botId: string; running: boolean; live: LiveRun | undefined; scope: Scope; nameOf: (botId: string) => string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const phrase = describeStep(step, nameOf);
  const row = stepKindOf(step.kind);
  const Icon = row?.icon ?? Icons.tool;
  const d = step.detail ?? {};
  const tone =
    step.kind === 'notice'
      ? d.level === 'warning' ? 'text-amber-700 dark:text-amber-300' : 'text-red-600 dark:text-red-400'
      : 'text-neutral-600 dark:text-neutral-300';

  if (row?.inline) return row.inline(step, { live, scope, t }, t(phrase.key, phrase.params));

  const body = row?.body?.(step, { live, scope, t }) ?? null;
  return (
    <div className={`text-xs ${tone}`}>
      <button
        type="button"
        disabled={!body}
        onClick={() => setOpen(!open)}
        aria-expanded={body ? open : undefined}
        className={cx('-mx-1.5 flex w-[calc(100%+0.75rem)] min-w-0 items-center gap-1.5 rounded-md px-1.5 py-0.5 text-left transition-colors enabled:hover:text-neutral-900 dark:enabled:hover:text-neutral-100', body ? interactive.hover : false)}
      >
        <Icon size={12} className="shrink-0" aria-hidden="true" />
        {typeof d.subAgentThread === 'string' && <Badge tone="info">{t('conversation.step.agent.badge')}</Badge>}
        <span className={step.kind === 'command' ? 'shrink-0' : 'min-w-0 truncate'}>{t(phrase.key, phrase.params)}</span>
        {step.kind === 'command' && typeof d.command === 'string' && <code className="min-w-0 truncate font-mono text-2xs text-neutral-500" title={d.command}>{displayCommand(d.command)}</code>}
        {step.kind === 'command' && typeof d.exitCode === 'number' && d.exitCode !== 0 && <span className="shrink-0 text-red-500">{t('conversation.step.exitCode', { code: d.exitCode })}</span>}
        <span className="ml-auto shrink-0"><StatusMark status={step.status} running={running} /></span>
      </button>
      {open && body && <div className="mt-1 pl-[18px]">{body}</div>}
      {/* An exhausted account or a spent balance is what switching models
          answers, so the failure row carries the entry to its picker. */}
      {botId !== '' && step.status === 'failed' && step.kind === 'notice' && (d.code === 'usageLimitExceeded' || d.failureCode === 'quota') && (
        <button
          type="button"
          onClick={() => openModelPicker(botId)}
          className="ml-[18px] mt-0.5 inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-xs font-medium text-signal-600 transition-colors hover:bg-signal-500/10 hover:text-signal-700 dark:text-signal-400 dark:hover:text-signal-300"
        >
          {t('conversation.step.switchModel')}
        </button>
      )}
    </div>
  );
}
