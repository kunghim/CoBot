import { describe, expect, it } from 'vitest';
import { modelsToProbe, visibleModels, withSelection } from './ModelPick';

describe('the model picker list', () => {
  it('sorts by name, numbers as numbers', () => {
    expect(visibleModels(['gpt-5.10', 'Claude-opus', 'gpt-5.4', 'deepseek-flash'], '')).toEqual([
      'Claude-opus',
      'deepseek-flash',
      'gpt-5.4',
      'gpt-5.10',
    ]);
  });

  it('keeps only the models a search names, ignoring case', () => {
    expect(visibleModels(['cursor/gpt-5.4-mini', 'glm-4.5', 'cursor/GPT-5.6-sol'], ' GPT ')).toEqual([
      'cursor/gpt-5.4-mini',
      'cursor/GPT-5.6-sol',
    ]);
  });

  it('does not reorder the list it was given', () => {
    const models = ['b', 'a'];
    visibleModels(models, '');
    expect(models).toEqual(['b', 'a']);
  });
});

describe('ticking and checking models', () => {
  it('ticks the models a search shows without losing the order of the ones already ticked', () => {
    expect(withSelection(['b'], ['a', 'b', 'c'], true)).toEqual(['b', 'a', 'c']);
  });

  it('clears only the models it is given', () => {
    expect(withSelection(['a', 'b', 'c'], ['a', 'c'], false)).toEqual(['b']);
  });

  it('asks only about the models that have not passed', () => {
    const answered = { a: { ok: true, code: 'ok' }, b: { ok: false, code: 'network' } };
    expect(modelsToProbe(['a', 'b', 'c'], answered)).toEqual(['b', 'c']);
    expect(modelsToProbe(['a'], answered)).toEqual([]);
  });
});
