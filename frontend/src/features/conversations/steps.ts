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
const collaborationTools = new Set([
  'send_message', 'create_task', 'propose_task', 'assign_task', 'update_task', 'submit_evidence',
  'report_completion', 'confirm_task', 'offer_transfer', 'accept_transfer', 'ask_user', 'request_member',
  'request_merge', 'remember', 'search_memory', 'get_context', 'list_tasks', 'get_task', 'list_roles',
]);

// Engine error codes with a plain-language explanation and what to do.
const knownErrorCodes = new Set([
  'serverOverloaded', 'usageLimitExceeded', 'rateLimitExceeded', 'contextWindowExceeded', 'unauthorized',
  'httpConnectionFailed', 'responseStreamConnectionFailed', 'responseStreamDisconnected', 'internalServerError',
]);

/** One-line description of a step. */
export function describeStep(step: RunStep, nameOf: (botId: string) => string): Phrase {
  const d = step.detail ?? {};
  switch (step.kind) {
    case 'reasoning':
      return { key: 'conversation.step.reasoning' };
    case 'command':
      return { key: step.status === 'in_progress' ? 'conversation.step.commandRunning' : 'conversation.step.command' };
    case 'file_change': {
      const changes = Array.isArray(d.changes) ? d.changes : [];
      return { key: 'conversation.step.fileChange', params: { count: changes.length } };
    }
    case 'web_search':
      return { key: 'conversation.step.webSearch', params: { query: text(d.query) } };
    case 'plan':
      return { key: 'conversation.step.plan' };
    case 'retry':
      return step.status === 'in_progress'
        ? { key: 'conversation.step.retrying', params: { attempt: Number(d.attempt) || 1 } }
        : { key: 'conversation.step.retried', params: { attempt: Number(d.attempt) || 1 } };
    case 'notice': {
      // Known engine error codes read in the user's language; the engine's
      // own text stays available in the step's detail.
      const code = text(d.code);
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
        const key = tool === 'send_message' && args.notifyOnly === true ? 'conversation.step.cobot.notify' : `conversation.step.cobot.${tool}`;
        return {
          key: collaborationTools.has(tool) ? key : 'conversation.step.tool',
          params: {
            name: names,
            title: text(args.title),
            status: text(args.status),
            question: text(args.question),
            query: text(args.query),
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

/** Steps worth listing: an empty thought would only add noise, and an
 * ongoing reconnect is already the status line itself. */
export function visibleSteps(steps: RunStep[], live: LiveRun | undefined): RunStep[] {
  return steps.filter((step) => {
    if (step.kind === 'reasoning') return text(step.detail?.text).trim() !== '' || (live?.reasoning[step.id] ?? '') !== '';
    if (step.kind === 'retry') return step.status !== 'in_progress';
    return true;
  });
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
export const slowAfterSeconds = 15;

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
