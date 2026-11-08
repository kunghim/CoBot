// Helpers for the process view: what a step says in plain language and what a
// running turn is doing right now.
import type { Run, RunStep, RunStepKind } from '../../api/types';
import type { LiveRun } from '../../api/live';
import { stepKindOf, stepKinds, type DescribedStep } from './stepKinds';
import { text, type Phrase } from './stepText';

export { displayCommand } from '../../components/content/displayCommand';
export { toolArguments, type Phrase } from './stepText';

/** One-line description of a step. */
export function describeStep(step: DescribedStep, nameOf: (botId: string) => string): Phrase {
  return stepKindOf(step.kind)?.describe(step, nameOf) ?? { key: 'conversation.step.other' };
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
 * work between the sayings. A stretch holds consecutive steps of one kind;
 * a step of another counted kind starts the next stretch, and steps that are
 * not counted on their own (a thought, a notice) follow along inside the
 * stretch they land in. */
export type TurnBlock = { kind: 'text'; step: RunStep } | { kind: 'work'; id: string; steps: RunStep[]; of?: string };

export function turnBlocks(steps: RunStep[]): TurnBlock[] {
  const out: TurnBlock[] = [];
  for (const step of steps) {
    if (step.kind === 'message') {
      out.push({ kind: 'text', step });
      continue;
    }
    const of = countedKinds.includes(step.kind) ? step.kind : undefined;
    const last = out[out.length - 1];
    if (last?.kind === 'work' && (of === undefined || last.of === of)) {
      last.steps.push(step);
      continue;
    }
    out.push({ kind: 'work', id: step.id, steps: [step], of });
  }
  return out;
}

const countedKinds: string[] = (Object.keys(stepKinds) as RunStepKind[]).filter((kind) => stepKinds[kind].counted);

/** One line for a stretch of work: how many of each it held. */
export function workSummary(steps: RunStep[]): Phrase[] {
  const out: Phrase[] = [];
  for (const kind of countedKinds) {
    const matching = steps.filter((step) => step.kind === kind);
    const paths = kind === 'file_change' ? matching.flatMap((step) => Array.isArray(step.detail.changes) ? step.detail.changes.flatMap((change: unknown) => change && typeof change === 'object' && 'path' in change && typeof change.path === 'string' ? [change.path] : []) : []) : [];
    const completePaths = matching.every((step) => Array.isArray(step.detail.changes) && step.detail.changes.length > 0 && step.detail.changes.every((change: unknown) => change && typeof change === 'object' && 'path' in change && typeof change.path === 'string'));
    const count = kind === 'file_change' && completePaths ? new Set(paths).size : matching.length;
    const label = kind === 'file_change' && !completePaths ? 'file_operation' : kind;
    if (count > 0) out.push({ key: `conversation.process.group.${label}`, params: { count } });
  }
  if (out.length === 0) out.push({ key: 'conversation.process.group.other', params: { count: steps.length } });
  return out;
}

/** Whether a stretch of work needs to be seen without a click: something went wrong in it. */
export function workNeedsAttention(steps: RunStep[]): boolean {
  return steps.some((step) => step.status === 'failed' || step.status === 'declined' || step.kind === 'notice');
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
  const phase = open && stepKindOf(open.kind)?.phase?.(open);
  if (phase) return phase;
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
