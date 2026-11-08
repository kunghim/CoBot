import { describe, expect, it } from 'vitest';
import type { RunStep, RunStepKind } from '../../api/types';
import { stepKindOf, stepKinds } from './stepKinds';
import { describeStep, runPhase } from './steps';

const step = (kind: string, detail: Record<string, unknown> = {}) => ({ id: 's', kind, status: 'in_progress', detail }) as unknown as RunStep;

describe('step kinds', () => {
  it('counts the same kinds as work as the core, in the order a summary lists them', () => {
    expect(Object.keys(stepKinds).filter((kind) => stepKinds[kind as RunStepKind].counted)).toEqual(['command', 'file_change', 'tool', 'web_search', 'agent']);
  });

  it('has nothing for a kind this build does not know', () => {
    for (const kind of ['invented', 'constructor', 'toString']) {
      expect(stepKindOf(kind)).toBeUndefined();
      expect(describeStep(step(kind), (id) => id)).toEqual({ key: 'conversation.step.other' });
    }
  });

  it('lets an open step say what the turn is doing, or leaves it to the rest of the turn', () => {
    const phase = (open: RunStep) => runPhase({ status: 'running' }, [open], undefined).key;
    expect(phase(step('command'))).toBe('conversation.phase.command');
    expect(runPhase({ status: 'running' }, [step('retry', { attempt: 2 })], undefined)).toEqual({ key: 'conversation.phase.reconnecting', params: { attempt: 2 } });
    for (const kind of ['reasoning', 'plan', 'agent', 'diff', 'notice', 'invented']) {
      expect(phase(step(kind))).toBe('conversation.phase.thinking');
    }
  });
});
