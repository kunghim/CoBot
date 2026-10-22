import { describe, expect, it } from 'vitest';
import { visibleModels } from './ModelPick';

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
