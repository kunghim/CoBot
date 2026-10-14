import { describe, expect, it } from 'vitest';
import { clampPagerIndex, optionFallbackText, optionLabelKeys } from './pager';

describe('approval pager math', () => {
  it('keeps the index inside the stack as cards disappear', () => {
    // Processing card 2 of 3 removes it; the index must now point at what
    // used to be card 3 without overflowing.
    expect(clampPagerIndex(1, 3)).toBe(1);
    expect(clampPagerIndex(2, 2)).toBe(1);
    expect(clampPagerIndex(5, 1)).toBe(0);
  });

  it('never goes below 0 and tolerates empty stacks and junk input', () => {
    expect(clampPagerIndex(-3, 2)).toBe(0);
    expect(clampPagerIndex(0, 0)).toBe(0);
    expect(clampPagerIndex(Number.NaN, 4)).toBe(0);
    expect(clampPagerIndex(1.9, 4)).toBe(1);
  });
});

describe('approval option labels', () => {
  it('prefers the engine-provided messageKey and keeps builtin fallbacks', () => {
    expect(optionLabelKeys({ id: 'accept', messageKey: 'approval.option.allow_once' })).toEqual([
      'approval.option.allow_once',
      'approval.option.allow_once',
      'approval.option.unknown',
    ]);
    expect(optionLabelKeys({ id: 'deny', messageKey: 'custom.key' })).toEqual([
      'custom.key',
      'approval.option.permissions.deny',
      'approval.option.unknown',
    ]);
  });

  it('falls back through id-based keys when the engine sends no messageKey', () => {
    expect(optionLabelKeys({ id: 'acceptForSession' })).toEqual([
      'approval.option.allow_thread_session',
      'approval.option.unknown',
    ]);
    expect(optionLabelKeys({ id: 'mystery' })).toEqual(['approval.option.unknown']);
  });

  it('never produces a blank button label', () => {
    expect(optionFallbackText({ id: 'accept' })).toBe('accept');
    expect(optionFallbackText({ id: 'accept', text: '允许' })).toBe('允许');
  });
});
