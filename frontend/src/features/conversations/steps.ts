// Pure helpers for the process view: what a step says in plain language and
// what a running turn is doing right now.
import type { Run, RunStep } from '../../api/types';
import type { LiveRun } from '../../api/live';

export interface Phrase {
  key: string;
  params?: Record<string, string | number>;
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

/** Tool arguments arrive as an object, or as a JSON string from some engines. */
export function toolArguments(detail: Record<string, unknown>): Record<string, unknown> {
  const raw = detail.arguments;
  if (raw && typeof raw === 'object') return raw as Record<string, unknown>;
  if (typeof raw === 'string') {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (parsed && typeof parsed === 'object') return parsed as Record<string, unknown>;
    } catch {
      // not JSON: no arguments to describe
    }
  }
  return {};
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

// Codex sub-agent tools and activity kinds with their own wording; anything
// newer reads as the generic sub-agent line instead of a raw key.
const agentTools = new Set(['spawnAgent', 'sendInput', 'sendMessage', 'followupTask', 'wait', 'listAgents', 'resumeAgent', 'interruptAgent', 'closeAgent']);
const agentActivities = new Set(['started', 'interacted', 'completed', 'interrupted']);

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

/** One-line description of a step. */
export function describeStep(step: { kind: string; status: string; detail: RunStep['detail'] }, nameOf: (botId: string) => string): Phrase {
  const d = step.detail ?? {};
  switch (step.kind) {
    case 'reasoning':
      return { key: 'conversation.step.reasoning' };
    case 'message':
      return { key: 'conversation.step.message' };
    case 'diff':
      return { key: 'conversation.step.diff', params: { count: Number(d.files) || 0 } };
    case 'agent': {
      // A member working with its own Codex sub-agents.
      const activity = text(d.activity);
      if (agentActivities.has(activity)) return { key: `conversation.step.agent.activity.${activity}`, params: { agent: text(d.agent) } };
      const tool = text(d.tool);
      return { key: `conversation.step.agent.${agentTools.has(tool) ? tool : 'other'}`, params: { count: Number(d.agents) || 1 } };
    }
    case 'command':
      if (step.status === 'declined') return { key: 'conversation.step.commandDeclined' };
      return { key: step.status === 'in_progress' ? 'conversation.step.commandRunning' : 'conversation.step.command' };
    case 'file_change': {
      const changes = Array.isArray(d.changes) ? d.changes : [];
      return { key: 'conversation.step.fileChange', params: { count: changes.length } };
    }
    case 'web_search':
      return { key: 'conversation.step.webSearch', params: { query: text(d.query) } };
    case 'plan':
      return { key: 'conversation.step.plan' };
    case 'compaction':
      return { key: step.status === 'in_progress' ? 'conversation.step.compacting' : 'conversation.step.compacted' };
    case 'retry':
      return step.status === 'in_progress'
        ? { key: 'conversation.step.retrying', params: { attempt: Number(d.attempt) || 1 } }
        : { key: 'conversation.step.retried', params: { attempt: Number(d.attempt) || 1 } };
    case 'notice': {
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
    case 'tool': {
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
    default:
      return { key: 'conversation.step.other' };
  }
}

/** Whether a thing the member said is one of the pieces the reply was made of
 * (the reply joins them with a blank line), not merely words the reply happens to contain. */
export function isPartOfReply(said: string, reply: string): boolean {
  const text = reply.trim();
  return text === said || text.startsWith(said + '\n\n') || text.endsWith('\n\n' + said) || text.includes('\n\n' + said + '\n\n');
}

/** Steps worth listing: an empty thought would only add noise, an ongoing
 * reconnect is already the status line itself, and what the member said that
 * the reply already holds (the closing words of the turn) would be said twice. */
export function visibleSteps(steps: RunStep[], live: LiveRun | undefined, replyText = ''): RunStep[] {
  return steps.filter((step) => {
    if (step.kind === 'reasoning') return text(step.detail?.text).trim() !== '' || (live?.reasoning[step.id] ?? '') !== '';
    if (step.kind === 'message') {
      const said = text(step.detail?.text).trim();
      if (said !== '' && isPartOfReply(said, replyText)) return false;
      return said !== '' || (live?.commentary[step.id] ?? '') !== '';
    }
    if (step.kind === 'retry') return step.status !== 'in_progress';
    return true;
  });
}

/** A turn read as the member lived it: what it said, and the stretches of
 * work between the sayings. */
export type TurnBlock = { kind: 'text'; step: RunStep } | { kind: 'work'; id: string; steps: RunStep[] };

export function turnBlocks(steps: RunStep[]): TurnBlock[] {
  const out: TurnBlock[] = [];
  for (const step of steps) {
    if (step.kind === 'message') {
      out.push({ kind: 'text', step });
      continue;
    }
    const last = out[out.length - 1];
    if (last?.kind === 'work') last.steps.push(step);
    else out.push({ kind: 'work', id: step.id, steps: [step] });
  }
  return out;
}

const countedKinds = ['command', 'file_change', 'tool', 'web_search', 'agent'] as const;

/** One line for a stretch of work: how many of each it held. */
export function workSummary(steps: RunStep[]): Phrase[] {
  const out: Phrase[] = [];
  for (const kind of countedKinds) {
    const count = steps.filter((step) => step.kind === kind).length;
    if (count > 0) out.push({ key: `conversation.process.group.${kind}`, params: { count } });
  }
  if (out.length === 0) out.push({ key: 'conversation.process.group.other', params: { count: steps.length } });
  return out;
}

/** Whether a stretch of work needs to be seen without a click: something went wrong in it. */
export function workNeedsAttention(steps: RunStep[]): boolean {
  return steps.some((step) => step.status === 'failed' || step.status === 'declined' || step.kind === 'notice');
}

/** The command a member ran, without the shell the engine wrapped it in:
 * `"C:\\...\\pwsh.exe" -Command 'git status'` reads as `git status`. */
export function displayCommand(command: string): string {
  const trimmed = command.trim();
  const wrapped =
    /^"?[^"]*?(?:pwsh|powershell)(?:\.exe)?"?\s+(?:-NoLogo\s+|-NoProfile\s+|-NonInteractive\s+)*-Command\s+([\s\S]+)$/i.exec(trimmed) ??
    /^"?[^"]*?\b(?:bash|zsh|sh)(?:\.exe)?"?\s+-l?c\s+([\s\S]+)$/i.exec(trimmed) ??
    /^"?[^"]*?\bcmd(?:\.exe)?"?\s+\/[cC]\s+([\s\S]+)$/.exec(trimmed);
  if (!wrapped) return trimmed;
  let inner = wrapped[1].trim();
  const quote = inner[0];
  if (inner.length >= 2 && (quote === "'" || quote === '"') && inner.at(-1) === quote) {
    // Inside single quotes a quote is written twice ('it''s').
    inner = quote === "'" ? inner.slice(1, -1).replace(/''/g, "'") : inner.slice(1, -1);
  }
  return inner;
}

/** How long a turn may go without any sign of progress before the status
 * line says the model is slow to answer. */
const slowAfterSeconds = 15;

/** What a running turn is doing now, for the status line in the flow.
 * `quietSeconds` is how long the turn has run without any sign of
 * progress; a long silence is called out instead of a bare "thinking". */
export function runPhase(run: Pick<Run, 'status'>, steps: RunStep[], live: LiveRun | undefined, quietSeconds = 0): Phrase {
  if (run.status === 'waiting_approval') return { key: 'conversation.phase.waitingApproval' };
  if (run.status === 'waiting_input') return { key: 'conversation.phase.waitingInput' };
  const open = [...steps].reverse().find((step) => step.status === 'in_progress');
  if (open?.kind === 'retry') return { key: 'conversation.phase.reconnecting', params: { attempt: Number(open.detail?.attempt) || 1 } };
  switch (open?.kind) {
    case 'command':
      return { key: 'conversation.phase.command' };
    case 'file_change':
      return { key: 'conversation.phase.editing' };
    case 'tool':
      return { key: 'conversation.phase.tool' };
    case 'web_search':
      return { key: 'conversation.phase.searching' };
    case 'message':
      return { key: 'conversation.phase.replying' };
    case 'compaction':
      return { key: 'conversation.phase.compacting' };
  }
  if ((live?.reply ?? '') !== '') return { key: 'conversation.phase.replying' };
  if (run.status === 'starting' && steps.length === 0) return { key: 'conversation.phase.starting' };
  if (steps.length === 0 && quietSeconds >= slowAfterSeconds) return { key: 'conversation.phase.slow' };
  return { key: 'conversation.phase.thinking' };
}

/** Elapsed whole seconds between two instants (never negative). */
export function elapsedSeconds(from: string, to: number): number {
  const start = Date.parse(from);
  if (Number.isNaN(start)) return 0;
  return Math.max(0, Math.floor((to - start) / 1000));
}

/** "8s" / "1m 23s" style durations via i18n keys. */
export function durationPhrase(seconds: number): Phrase {
  if (seconds < 60) return { key: 'conversation.duration.seconds', params: { s: seconds } };
  return { key: 'conversation.duration.minutes', params: { m: Math.floor(seconds / 60), s: seconds % 60 } };
}
