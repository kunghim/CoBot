import { describe, expect, it } from 'vitest';
import { approvalSummaryText, clampPagerIndex, optionFallbackText, optionLabelKeys } from './pager';

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
      'approval.option.allow_session',
      'approval.option.unknown',
    ]);
    expect(optionLabelKeys({ id: 'mystery' })).toEqual(['approval.option.unknown']);
  });

  it('never produces a blank button label', () => {
    expect(optionFallbackText({ id: 'accept' })).toBe('accept');
    expect(optionFallbackText({ id: 'accept', text: '允许' })).toBe('允许');
  });
});

describe('approvalSummaryText', () => {
  const t = (key: string) => `zh:${key}`; // a real catalog resolves keys; raw pass-through never reaches t

  it('renders product-stored i18n keys through t()', () => {
    expect(approvalSummaryText(t, 'approval.card.chain_risk')).toBe('zh:approval.card.chain_risk');
    expect(approvalSummaryText(t, 'approval.card.recovery')).toBe('zh:approval.card.recovery');
  });

  it('passes engine-provided summaries through untranslated', () => {
    expect(approvalSummaryText(t, 'rm -rf /tmp/cache')).toBe('rm -rf /tmp/cache');
    expect(approvalSummaryText(t, '合并 something')).toBe('合并 something');
  });

  it('falls back to the raw key when a key is missing in the catalog', () => {
    const bare = (key: string) => key; // no translation available
    expect(approvalSummaryText(bare, 'approval.card.unknown')).toBe('approval.card.unknown');
  });
});
