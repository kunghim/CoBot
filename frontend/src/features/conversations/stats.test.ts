import { describe, expect, it } from 'vitest';
import { compactTokens, percent } from './SessionStatsBar';

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
});
