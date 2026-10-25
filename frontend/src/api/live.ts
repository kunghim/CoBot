// The live store holds what running turns are doing right now: the newest
// form of each step and the text streamed so far. It is fed by `cobot.live`
// stream frames and is never a source of truth — stored steps from the
// conversation query win once they are newer, and a finished run's live
// state is dropped.
import { useSyncExternalStore } from 'react';
import type { LiveSignal, RunStep } from './types';

export interface LiveRun {
  steps: Record<string, RunStep>;
  /** The answer streamed so far (the turn's final-answer items, in order). */
  reply: string;
  /** Streamed thinking / command output / working remarks per step id. */
  reasoning: Record<string, string>;
  commentary: Record<string, string>;
  output: Record<string, string>;
}

const maxStream = 64 * 1024;
let runs: Record<string, LiveRun> = {};
let chatRuns: Record<string, LiveRun> = {};
const listeners = new Set<() => void>();

function emit() {
  for (const listener of listeners) listener();
}

function clip(text: string): string {
  return text.length > maxStream ? text.slice(text.length - maxStream) : text;
}

export function applyLiveSignal(signal: LiveSignal): void {
  if (!signal.runId) return;
  const collection = signal.chatId ? chatRuns : runs;
  const current = collection[signal.runId] ?? { steps: {}, reply: '', reasoning: {}, output: {}, commentary: {} };
  let next: LiveRun = current;
  if (signal.type === 'step' && signal.step) {
    next = { ...current, steps: { ...current.steps, [signal.step.id]: signal.step } };
  } else if (signal.type === 'delta' && signal.text) {
    const id = signal.itemId ?? '';
    if (signal.kind === 'message') next = { ...current, reply: clip(current.reply + signal.text) };
    else if (signal.kind === 'reasoning') next = { ...current, reasoning: { ...current.reasoning, [id]: clip((current.reasoning[id] ?? '') + signal.text) } };
    else if (signal.kind === 'commentary') next = { ...current, commentary: { ...current.commentary, [id]: clip((current.commentary[id] ?? '') + signal.text) } };
    else if (signal.kind === 'output') next = { ...current, output: { ...current.output, [id]: clip((current.output[id] ?? '') + signal.text) } };
  }
  if (next === current) return;
  if (signal.chatId) chatRuns = { ...chatRuns, [signal.runId]: next };
  else runs = { ...runs, [signal.runId]: next };
  emit();
}

/** Drops live state of runs that are no longer running. */
export function forgetLiveRuns(keep: (runId: string) => boolean): void {
  const kept = Object.fromEntries(Object.entries(runs).filter(([id]) => keep(id)));
  if (Object.keys(kept).length === Object.keys(runs).length) return;
  runs = kept;
  emit();
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function liveRunsSnapshot(): Record<string, LiveRun> {
  return runs;
}

export function useLiveRuns(): Record<string, LiveRun> {
  return useSyncExternalStore(subscribe, liveRunsSnapshot, liveRunsSnapshot);
}

export function useChatLiveRuns(): Record<string, LiveRun> {
  return useSyncExternalStore(subscribe, () => chatRuns, () => chatRuns);
}

export function forgetChatRun(id: string): void {
  if (!chatRuns[id]) return;
  const next = { ...chatRuns }; delete next[id]; chatRuns = next; emit();
}

/** Stored steps merged with live ones; the newer form of a step wins. */
export function mergeSteps(stored: RunStep[], live: LiveRun | undefined): RunStep[] {
  const byId = new Map(stored.map((step) => [step.id, step]));
  for (const step of Object.values(live?.steps ?? {})) {
    const existing = byId.get(step.id);
    if (!existing || Date.parse(step.updatedAt) >= Date.parse(existing.updatedAt)) byId.set(step.id, step);
  }
  return [...byId.values()].sort((a, b) => a.seq - b.seq);
}

/** Test seam: resets the store between cases. */
export function resetLiveStore(): void {
  runs = {};
  chatRuns = {};
  emit();
}
