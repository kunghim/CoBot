// RunProcess shows what a member did in one turn, the way Codex does: while
// it runs, a live status line ("正在执行命令 · 12秒") with the steps so far and
// the reply as it streams; once finished, a collapsed "已处理 1分23秒" line that
// expands into the same steps.
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Check, Loader2, Minus, X } from 'lucide-react';
import { Badge, Button, cx, Icons, interactive } from '../../components/ui';
import type { RunStep, RunWithSteps } from '../../api/types';
import type { LiveRun } from '../../api/live';
import { mergeSteps } from '../../api/live';
import { api } from '../../api/client';
import { apiErrorMessage } from '../../i18n/errors';
import DiffView from '../../components/DiffView';
import Markdown from './Markdown';
import { describeStep, displayCommand, durationPhrase, elapsedSeconds, runPhase, toolArguments, visibleSteps, type Phrase } from './steps';

function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);
  return now;
}

export default function RunProcess({
  run,
  active,
  live,
  teamId,
  nameOf,
  onRetry,
  retrying = false,
}: {
  run: RunWithSteps;
  active: boolean;
  live: LiveRun | undefined;
  teamId: string;
  nameOf: (botId: string) => string;
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
  const [open, setOpen] = useState<boolean | null>(null);
  const steps = mergeSteps(run.steps, active ? live : undefined);
  const shown = visibleSteps(steps, active ? live : undefined);
  // A failed turn stays open so its reason is seen without a click.
  const expanded = open ?? (active || run.status === 'failed');

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

  const endedAt = run.endedAt ? Date.parse(run.endedAt) : NaN;
  const seconds = elapsedSeconds(run.startedAt, active || Number.isNaN(endedAt) ? now : endedAt);
  const duration = say(durationPhrase(seconds));
  const header = active
    ? `${say(runPhase(run, steps, live, seconds))} · ${duration}`
    : t(run.status === 'failed' ? 'conversation.process.failedAfter' : run.status === 'interrupted' ? 'conversation.process.interruptedAfter' : 'conversation.process.done', { duration, count: shown.length });

  return (
    <div className="max-w-[85%] space-y-1.5" data-testid="run-process">
      <button
        type="button"
        onClick={() => setOpen(!expanded)}
        aria-expanded={expanded}
        className={cx('-ml-1.5 flex items-center gap-1.5 rounded-lg px-1.5 py-0.5 text-xs text-neutral-500 transition-colors hover:text-neutral-800 dark:text-neutral-400 dark:hover:text-neutral-200', interactive.hover)}
      >
        {active ? <Loader2 size={13} className="animate-spin text-signal-500" aria-hidden="true" /> : expanded ? <Icons.expand size={13} aria-hidden /> : <Icons.next size={13} aria-hidden />}
        <span role={active ? 'status' : undefined}>{header}</span>
      </button>
      {expanded && shown.length > 0 && (
        <ol className="ml-1.5 space-y-1 border-l border-neutral-200 pl-3 dark:border-neutral-700">
          {shown.map((step) => (
            <li key={step.id}>
              <StepRow step={step} running={active && step.status === 'in_progress'} live={active ? live : undefined} teamId={teamId} nameOf={nameOf} />
            </li>
          ))}
        </ol>
      )}
      {retry}
      {active && live?.reply && (
        <div className="text-sm leading-7 text-neutral-900 dark:text-neutral-100" data-testid="streaming-reply">
          <Markdown text={live.reply} teamId={teamId} />
        </div>
      )}
    </div>
  );
}

const kindIcon: Record<string, (typeof Icons)[keyof typeof Icons]> = {
  reasoning: Icons.thinking,
  command: Icons.command,
  file_change: Icons.fileChange,
  tool: Icons.tool,
  web_search: Icons.webSearch,
  plan: Icons.plan,
  retry: Icons.retry,
  notice: Icons.warning,
  agent: Icons.subagent,
  diff: Icons.diff,
  compaction: Icons.compaction,
};

function StatusMark({ status, running }: { status: string; running: boolean }) {
  if (running) return <Loader2 size={11} className="animate-spin text-neutral-400" aria-hidden="true" />;
  if (status === 'failed' || status === 'declined') return <X size={11} className="text-red-500" aria-hidden="true" />;
  if (status === 'interrupted') return <Minus size={11} className="text-neutral-400" aria-hidden="true" />;
  return <Check size={11} className="text-neutral-400" aria-hidden="true" />;
}

function StepRow({ step, running, live, teamId, nameOf }: { step: RunStep; running: boolean; live: LiveRun | undefined; teamId: string; nameOf: (botId: string) => string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const phrase = describeStep(step, nameOf);
  const Icon = kindIcon[step.kind] ?? Icons.tool;
  const d = step.detail ?? {};
  const tone =
    step.kind === 'notice'
      ? d.level === 'warning' ? 'text-amber-700 dark:text-amber-300' : 'text-red-600 dark:text-red-400'
      : 'text-neutral-600 dark:text-neutral-300';

  // What the model says while it works reads as plain text in its place.
  if (step.kind === 'message') {
    const said = (typeof d.text === 'string' && d.text) || live?.commentary[step.id] || '';
    return (
      <div className="text-sm text-neutral-800 dark:text-neutral-200" data-testid="run-commentary">
        <Markdown text={said} teamId={teamId} />
      </div>
    );
  }

  if (step.kind === 'reasoning') {
    const thought = (typeof d.text === 'string' && d.text) || live?.reasoning[step.id] || '';
    return (
      <div className="text-xs text-neutral-500 dark:text-neutral-400">
        <p className="flex items-center gap-1.5"><Icon size={12} aria-hidden="true" />{t(phrase.key, phrase.params)}</p>
        <div className="mt-0.5 pl-[18px] italic"><Markdown text={thought} teamId={teamId} /></div>
      </div>
    );
  }

  const body = stepBody(step, live, teamId, t);
  return (
    <div className={`text-xs ${tone}`}>
      <button
        type="button"
        disabled={!body}
        onClick={() => setOpen(!open)}
        aria-expanded={body ? open : undefined}
        className={cx('-mx-1.5 flex w-[calc(100%+0.75rem)] min-w-0 items-center gap-1.5 rounded-md px-1.5 py-0.5 text-left transition-colors enabled:hover:text-neutral-900 dark:enabled:hover:text-neutral-100', body && interactive.hover)}
      >
        <Icon size={12} className="shrink-0" aria-hidden="true" />
        {typeof d.subAgentThread === 'string' && <Badge tone="info">{t('conversation.step.agent.badge')}</Badge>}
        <span className={step.kind === 'command' ? 'shrink-0' : 'min-w-0 truncate'}>{t(phrase.key, phrase.params)}</span>
        {step.kind === 'command' && typeof d.command === 'string' && <code className="min-w-0 truncate font-mono text-2xs text-neutral-500" title={d.command}>{displayCommand(d.command)}</code>}
        {step.kind === 'command' && typeof d.exitCode === 'number' && d.exitCode !== 0 && <span className="shrink-0 text-red-500">{t('conversation.step.exitCode', { code: d.exitCode })}</span>}
        <span className="ml-auto shrink-0"><StatusMark status={step.status} running={running} /></span>
      </button>
      {open && body && <div className="mt-1 pl-[18px]">{body}</div>}
    </div>
  );
}

function stepBody(step: RunStep, live: LiveRun | undefined, teamId: string, t: (key: string, params?: Record<string, unknown>) => string) {
  const d = step.detail ?? {};
  switch (step.kind) {
    case 'command': {
      const output = (typeof d.output === 'string' ? d.output : '') || live?.output[step.id] || '';
      return (
        <div className="space-y-1">
          <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-lg bg-neutral-900/[0.04] p-2 font-mono text-2xs leading-4 text-neutral-800 dark:bg-black/30 dark:text-neutral-100">$ {displayCommand(String(d.command ?? ''))}</pre>
          {output ? <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-neutral-900/[0.04] p-2 font-mono text-2xs leading-4 text-neutral-700 dark:bg-black/30 dark:text-neutral-300">{output}</pre> : <p className="text-neutral-500 dark:text-neutral-400">{t('conversation.step.noOutput')}</p>}
        </div>
      );
    }
    case 'file_change': {
      const changes = Array.isArray(d.changes) ? (d.changes as Array<Record<string, unknown>>) : [];
      return (
        <ul className="space-y-1">
          {changes.map((change, index) => <FileChangeRow key={index} change={change} teamId={teamId} />)}
        </ul>
      );
    }
    case 'tool': {
      const args = toolArguments(d);
      const message = typeof args.text === 'string' ? args.text : typeof args.question === 'string' ? args.question : typeof args.description === 'string' ? args.description : '';
      const extra = typeof d.error === 'string' ? d.error : '';
      if (!message && !extra && Object.keys(args).length === 0) return null;
      return (
        <div className="space-y-1">
          {message ? (
            <div className="rounded bg-neutral-100 px-2 py-1 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-200"><Markdown text={message} teamId={teamId} /></div>
          ) : (
            <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-neutral-100 p-2 font-mono text-2xs dark:bg-neutral-800">{JSON.stringify(args, null, 2)}</pre>
          )}
          {extra && <p className="text-red-600">{extra}</p>}
        </div>
      );
    }
    case 'plan':
      return typeof d.text === 'string' && d.text ? <Markdown text={d.text} teamId={teamId} /> : null;
    case 'diff':
      return typeof d.diff === 'string' && d.diff ? <DiffView diff={d.diff} /> : null;
    case 'agent':
      return typeof d.prompt === 'string' && d.prompt ? <div className="rounded bg-neutral-100 px-2 py-1 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-200"><Markdown text={d.prompt} teamId={teamId} /></div> : null;
    case 'retry':
    case 'notice':
      return typeof d.message === 'string' && d.message ? <p className="break-all text-neutral-500 dark:text-neutral-400">{d.message}</p> : null;
    default:
      return null;
  }
}

function FileChangeRow({ change, teamId }: { change: Record<string, unknown>; teamId: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [error, setError] = useState('');
  const path = typeof change.path === 'string' ? change.path : '';
  const kind = typeof change.kind === 'string' ? change.kind : 'update';
  const diff = typeof change.diff === 'string' ? change.diff : '';
  return (
    <li>
      <button type="button" onClick={() => setOpen(!open)} aria-expanded={open} className={cx('-mx-1 flex min-w-0 items-center gap-1.5 rounded-md px-1 py-0.5 text-left transition-colors hover:text-neutral-900 dark:hover:text-neutral-100', interactive.hover)}>
        {open ? <Icons.expand size={11} aria-hidden /> : <Icons.next size={11} aria-hidden />}
        <span className="shrink-0 rounded bg-neutral-900/[0.06] px-1 text-2xs dark:bg-white/[0.08]">{t(`conversation.fileKind.${kind}`, { defaultValue: kind })}</span>
        <span className="min-w-0 truncate font-mono text-2xs">{path}</span>
      </button>
      {open && (
        <div className="mt-1 space-y-1">
          <Button
            variant="ghost"
            size="sm"
            icon={Icons.openExternal}
            onClick={() => {
              setError('');
              api.openTeamPath(teamId, path).catch((err) => setError(apiErrorMessage(t, err)));
            }}
          >
            {t('conversation.file.open')}
          </Button>
          {error && <span role="alert" className="ml-2 text-red-600 dark:text-red-400">{error}</span>}
          {diff && <DiffView diff={diff} />}
        </div>
      )}
    </li>
  );
}
