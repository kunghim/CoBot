import { describe, expect, it } from 'vitest';
import { currentModel } from './ReasoningEffortSelect';

describe('currentModel', () => {
  const models = [
    { slug: 'a', reasoningEfforts: [{ effort: 'low' }] },
    { slug: 'b', isDefault: true, defaultReasoningEffort: 'medium', reasoningEfforts: [{ effort: 'medium' }, { effort: 'high' }] },
  ];
  it('uses the configured model, else the engine default, else the first', () => {
    expect(currentModel(models, 'a')?.slug).toBe('a');
    expect(currentModel(models, '')?.slug).toBe('b');
    expect(currentModel(models, 'missing')?.slug).toBe('b');
    expect(currentModel([{ slug: 'only' }], undefined)?.slug).toBe('only');
    expect(currentModel([], 'a')).toBeUndefined();
  });
});
