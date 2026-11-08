import { describe, expect, it } from 'vitest';
import { clampPagerIndex } from './useCardPager';

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
