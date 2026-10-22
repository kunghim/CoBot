import { describe, expect, it } from 'vitest';
import { compactTokens, percent, tightestLimit } from './SessionStatsBar';

describe('session stats formatting', () => {
  it('shortens token counts like Codex', () => {
    expect(compactTokens(536)).toBe('536');
    expect(compactTokens(1_536)).toBe('1.5K');
    expect(compactTokens(34_300)).toBe('34K');
    expect(compactTokens(1_813_113)).toBe('1.8M');
    expect(compactTokens(82_160_472)).toBe('82M');
  });
  it('rounds percentages and never divides by zero', () => {
    expect(percent(69_307_821, 82_160_472)).toBe(84);
    expect(percent(5, 0)).toBe(0);
  });
  it('shows the window closest to running out, not always the weekly one', () => {
    const fiveHours = { usedPercent: 95, windowDurationMins: 300 };
    const week = { usedPercent: 18, windowDurationMins: 10080 };
    expect(tightestLimit({ primary: fiveHours, secondary: week })).toBe(fiveHours);
    expect(tightestLimit({ primary: { usedPercent: 10, windowDurationMins: 300 }, secondary: week })).toBe(week);
    expect(tightestLimit({ secondary: week })).toBe(week);
    expect(tightestLimit({})).toBeUndefined();
  });
});
