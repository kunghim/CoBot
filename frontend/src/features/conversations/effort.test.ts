import { describe, expect, it } from 'vitest';
import { currentModel, effortChoices } from './ReasoningEffortSelect';

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

describe('effortChoices', () => {
  const engine = [
    { slug: 'gpt', isDefault: true, defaultReasoningEffort: 'medium', reasoningEfforts: [{ effort: 'low' }, { effort: 'medium' }] },
    { slug: 'gpt-max', reasoningEfforts: [{ effort: 'high' }] },
  ];
  const account = { id: 'openai', authMode: 'chatgpt', models: [] } as never;
  const gateway = (model: Record<string, unknown>) => ({ id: 'mimo', authMode: 'api_key', models: [{ id: 'mimo-flash', ...model }] }) as never;

  it('gives the ChatGPT account the chosen model\'s own levels', () => {
    expect(effortChoices(account, 'gpt-max', engine).levels).toEqual([{ effort: 'high' }]);
    expect(effortChoices(account, 'gpt', engine).defaultEffort).toBe('medium');
  });

  it('gives another vendor\'s model low, medium and high unless its configuration lists others', () => {
    expect(effortChoices(gateway({}), 'mimo-flash', engine)).toEqual({ levels: [{ effort: 'low' }, { effort: 'medium' }, { effort: 'high' }], toggle: false, defaultEffort: '' });
    const declared = effortChoices(gateway({ supportedEfforts: ['low', 'high'], defaultEffort: 'low' }), 'mimo-flash', engine);
    expect(declared.levels).toEqual([{ effort: 'low' }, { effort: 'high' }]);
    expect(declared.defaultEffort).toBe('low');
    // A model the provider does not list has none either.
    expect(effortChoices(gateway({ supportedEfforts: ['low'] }), 'other', engine).levels).toEqual([]);
  });

  it('uses the engine\'s own model when no provider row stands behind the choice', () => {
    expect(effortChoices(undefined, '', engine).levels).toEqual([{ effort: 'low' }, { effort: 'medium' }]);
  });
});
