// One row for each kind of step a turn is made of: how it reads and what opens
// under it. Keyed by the interface's own list, so a kind added there does not
// compile until it is here.
import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, cx, Icons, interactive } from '../../components/ui';
import type { RunStep, RunStepKind } from '../../api/types';
import type { LiveRun } from '../../api/live';
import { openWorkspaceFile } from '../workspace/fileNavigation';
import { apiErrorMessage } from '../../i18n/errors';
import DiffView from '../../components/DiffView';
import Markdown from '../../components/content/Markdown';
import { stripToolCallText } from './toolCallText';
import { displayCommand } from '../../components/content/displayCommand';
import { text, toolArguments, type Phrase } from './stepText';

/** Where the files a turn names live: a team's folder, or a chat's own. */
export interface Scope {
  teamId?: string;
  chatId?: string;
}

/** What describing a step looks at. */
export type DescribedStep = { kind: string; status: string; detail: RunStep['detail'] };

export interface StepContext {
  live: LiveRun | undefined;
  scope: Scope;
  t: (key: string, params?: Record<string, unknown>) => string;
}

export interface StepKind {
  icon: (typeof Icons)[keyof typeof Icons];
  describe: (step: DescribedStep, nameOf: (botId: string) => string) => Phrase;
  // What the status line says while a step of this kind is open. Absent: the
  // line goes by the rest of the turn.
  phase?: (step: RunStep) => Phrase;
  // Counted when summing up a stretch of work: the member doing something, the
  // same kinds as the core's IsWorkStep. The rows' order is the summary's.
  counted?: boolean;
  // What opens under the step's line.
  body?: (step: RunStep, context: StepContext) => ReactNode;
  // Shown as it is in place of a line that opens.
  inline?: (step: RunStep, context: StepContext, label: string) => ReactNode;
}

const says = (key: string, params?: Phrase['params']): Phrase => (params ? { key, params } : { key });
const always = (key: string) => () => says(key);

export const stepKinds: Record<RunStepKind, StepKind> = {
  reasoning: {
    icon: Icons.thinking,
    describe: always('conversation.step.reasoning'),
    inline: (step, { live, scope }, label) => {
      const thought = text(step.detail?.text) || live?.reasoning[step.id] || '';
      return (
        <div className="text-xs text-neutral-500 dark:text-neutral-400">
          <p className="flex items-center gap-1.5"><Icons.thinking size={12} aria-hidden="true" />{label}</p>
          <div className="mt-0.5 pl-[18px] italic"><Markdown text={thought} teamId={scope.teamId} chatId={scope.chatId} /></div>
        </div>
      );
    },
  },
  command: {
    icon: Icons.command,
    counted: true,
    describe: (step) => {
      if (step.status === 'declined') return says('conversation.step.commandDeclined');
      return says(step.status === 'in_progress' ? 'conversation.step.commandRunning' : 'conversation.step.command');
    },
    phase: always('conversation.phase.command'),
    body: (step, { live, t }) => {
      const d = step.detail ?? {};
      const output = text(d.output) || live?.output[step.id] || '';
      return (
        <div className="space-y-1">
          <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-lg bg-neutral-900/[0.04] p-2 font-mono text-2xs leading-4 text-neutral-800 dark:bg-black/30 dark:text-neutral-100">$ {displayCommand(String(d.command ?? ''))}</pre>
          {output ? <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-neutral-900/[0.04] p-2 font-mono text-2xs leading-4 text-neutral-700 dark:bg-black/30 dark:text-neutral-300">{output}</pre> : <p className="text-neutral-500 dark:text-neutral-400">{t('conversation.step.noOutput')}</p>}
        </div>
      );
    },
  },
  file_change: {
    icon: Icons.fileChange,
    counted: true,
    describe: (step) => says('conversation.step.fileChange', { count: Array.isArray(step.detail?.changes) ? step.detail.changes.length : 0 }),
    phase: always('conversation.phase.editing'),
    body: (step, { scope }) => {
      const d = step.detail ?? {};
      const changes = Array.isArray(d.changes) ? (d.changes as Array<Record<string, unknown>>) : [];
      return (
        <ul className="space-y-1">
          {changes.map((change, index) => <FileChangeRow key={index} change={change} scope={scope} />)}
        </ul>
      );
    },
  },
  tool: {
    icon: Icons.tool,
    counted: true,
    describe: describeTool,
    phase: always('conversation.phase.tool'),
    body: (step, { scope }) => {
      const d = step.detail ?? {};
      const args = toolArguments(d);
      const message = typeof args.text === 'string' ? args.text : typeof args.question === 'string' ? args.question : typeof args.description === 'string' ? args.description : '';
      const extra = text(d.error);
      if (!message && !extra && Object.keys(args).length === 0) return null;
      return (
        <div className="space-y-1">
          {message ? (
            <div className="rounded bg-neutral-100 px-2 py-1 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-200"><Markdown text={message} teamId={scope.teamId} chatId={scope.chatId} /></div>
          ) : (
            <pre className="max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-neutral-100 p-2 font-mono text-2xs dark:bg-neutral-800">{JSON.stringify(args, null, 2)}</pre>
          )}
          {extra && <p className="text-red-600">{extra}</p>}
        </div>
      );
    },
  },
  web_search: {
    icon: Icons.webSearch,
    counted: true,
    describe: (step) => says('conversation.step.webSearch', { query: text(step.detail?.query) }),
    phase: always('conversation.phase.searching'),
  },
  plan: {
    icon: Icons.plan,
    describe: always('conversation.step.plan'),
    body: (step, { scope }) => (text(step.detail?.text) ? <Markdown text={text(step.detail?.text)} teamId={scope.teamId} chatId={scope.chatId} /> : null),
  },
  // What the model says while it works reads as plain text in its place.
  message: {
    icon: Icons.tool,
    describe: always('conversation.step.message'),
    phase: always('conversation.phase.replying'),
    inline: (step, { live, scope }) => {
      const said = text(step.detail?.text) || live?.commentary[step.id] || '';
      return (
        <div className="text-sm text-neutral-800 dark:text-neutral-200" data-testid="run-commentary">
          <Markdown text={stripToolCallText(said)} teamId={scope.teamId} chatId={scope.chatId} />
        </div>
      );
    },
  },
  // A member working with its own Codex sub-agents.
  agent: {
    icon: Icons.subagent,
    counted: true,
    describe: describeAgent,
    body: (step, { scope }) => (text(step.detail?.prompt) ? <div className="rounded bg-neutral-100 px-2 py-1 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-200"><Markdown text={text(step.detail?.prompt)} teamId={scope.teamId} chatId={scope.chatId} /></div> : null),
  },
  diff: {
    icon: Icons.diff,
    describe: (step) => says('conversation.step.diff', { count: Number(step.detail?.files) || 0 }),
    body: (step) => (text(step.detail?.diff) ? <DiffView diff={text(step.detail?.diff)} /> : null),
  },
  compaction: {
    icon: Icons.compaction,
    describe: (step) => says(step.status === 'in_progress' ? 'conversation.step.compacting' : 'conversation.step.compacted'),
    phase: always('conversation.phase.compacting'),
  },
  notice: {
    icon: Icons.warning,
    describe: describeNotice,
    body: messageBody,
  },
  retry: {
    icon: Icons.retry,
    describe: (step) => says(step.status === 'in_progress' ? 'conversation.step.retrying' : 'conversation.step.retried', { attempt: Number(step.detail?.attempt) || 1 }),
    phase: (step) => says('conversation.phase.reconnecting', { attempt: Number(step.detail?.attempt) || 1 }),
    body: messageBody,
  },
};

/** The row of a step; a kind this build does not know has none. */
export function stepKindOf(kind: string): StepKind | undefined {
  return Object.hasOwn(stepKinds, kind) ? stepKinds[kind as RunStepKind] : undefined;
}

// CoBot's own collaboration tools, described as what the member did.
const collaborationTools = new Set(['start_requirement', 'handoff', 'view_tasks', 'send_message', 'ask_user', 'browser']);

/** The address a browser call opens, when it is in one of its steps. */
function browserTarget(steps: unknown): string {
  if (!Array.isArray(steps)) return '';
  for (const step of steps) {
    if (step && typeof step === 'object' && typeof (step as { url?: unknown }).url === 'string') return (step as { url: string }).url;
  }
  return '';
}

function describeTool(step: DescribedStep, nameOf: (botId: string) => string): Phrase {
  const d = step.detail ?? {};
  const tool = text(d.tool);
  if (text(d.server) === 'cobot' || collaborationTools.has(tool)) {
    const args = toolArguments(d);
    const recipients = Array.isArray(args.targetBotIds) ? args.targetBotIds.filter((id): id is string => typeof id === 'string') : [];
    const target = text(args.targetBotId) || text(args.ownerId) || text(args.toBotId);
    const names = (recipients.length ? recipients : target ? [target] : []).map(nameOf).join('、');
    const outcome = text(args.outcome);
    const key =
      tool === 'handoff' ? `conversation.step.cobot.handoff_${['back', 'escalate', 'resume'].includes(outcome) ? outcome : 'done'}`
      : `conversation.step.cobot.${tool}`;
    return {
      key: collaborationTools.has(tool) ? key : 'conversation.step.tool',
      params: {
        name: names,
        title: text(args.title),
        status: text(args.status),
        question: text(args.question),
        query: text(args.query),
        url: text(args.url) || browserTarget(args.steps),
        tool,
      },
    };
  }
  return { key: 'conversation.step.tool', params: { tool: [text(d.server), tool].filter(Boolean).join('.') } };
}

// Codex sub-agent tools and activity kinds with their own wording; anything
// newer reads as the generic sub-agent line instead of a raw key.
const agentTools = new Set(['spawnAgent', 'sendInput', 'sendMessage', 'followupTask', 'wait', 'listAgents', 'resumeAgent', 'interruptAgent', 'closeAgent']);
const agentActivities = new Set(['started', 'interacted', 'completed', 'interrupted']);

function describeAgent(step: DescribedStep): Phrase {
  const d = step.detail ?? {};
  const activity = text(d.activity);
  if (agentActivities.has(activity)) return { key: `conversation.step.agent.activity.${activity}`, params: { agent: text(d.agent) } };
  const tool = text(d.tool);
  return { key: `conversation.step.agent.${agentTools.has(tool) ? tool : 'other'}`, params: { count: Number(d.agents) || 1 } };
}

// Engine error codes with a plain-language explanation and what to do.
const knownErrorCodes = new Set([
  'serverOverloaded', 'usageLimitExceeded', 'rateLimitExceeded', 'contextWindowExceeded', 'unauthorized',
  'httpConnectionFailed', 'responseStreamConnectionFailed', 'responseStreamDisconnected', 'internalServerError',
]);

// Root causes taken from a gateway's own HTTP status. They say what the
// provider refused and why, which the engine's symptom codes cannot: a
// balance that ran out, a key that was refused and a model that does not
// exist all reach the engine as "the request failed".
const providerFailureCodes = new Set([
  'quota', 'rate_limit', 'auth_invalid', 'model_unknown', 'model_unavailable', 'context_exceeded',
  'protocol_unsupported', 'request_rejected', 'network', 'engine_internal',
]);

function describeNotice(step: DescribedStep): Phrase {
  const d = step.detail ?? {};
  // Known engine error codes read in the user's language; the engine's
  // own text stays available in the step's detail.
  const code = text(d.code);
  // A third-party gateway answers with its own status, and that status
  // is the root cause: an empty balance and a rejected key both arrive
  // as "the request failed". It comes first because it names the reason
  // rather than the symptom.
  const failureCode = text(d.failureCode);
  if (d.level !== 'warning' && providerFailureCodes.has(failureCode)) {
    return { key: `conversation.providerError.${failureCode}` };
  }
  if (d.level !== 'warning' && knownErrorCodes.has(code)) return { key: `conversation.engineError.${code}` };
  return { key: d.level === 'warning' ? 'conversation.step.warning' : 'conversation.step.error', params: { message: text(d.message) } };
}

function messageBody(step: RunStep): ReactNode {
  const message = text(step.detail?.message);
  return message ? <p className="break-all text-neutral-500 dark:text-neutral-400">{message}</p> : null;
}

function FileChangeRow({ change, scope }: { change: Record<string, unknown>; scope: Scope }) {
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
              openWorkspaceFile(scope.chatId ? { kind: 'chat', id: scope.chatId } : { kind: 'team', id: scope.teamId ?? '' }, path).catch((err) => setError(apiErrorMessage(t, err)));
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
