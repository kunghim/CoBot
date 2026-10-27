import { describe, expect, it } from 'vitest';
import { defaultTokens, formatTokens, fromUnits, scaleOf, toUnits, unitLabelKey, unitSize } from './budgetUnits';

describe('the daily budget units', () => {
  it('counts a team in 亿 and a member in 千万, starting at one unit', () => {
    expect(defaultTokens('team')).toBe(100_000_000);
    expect(defaultTokens('bot')).toBe(10_000_000);
  });

  it('converts between units and tokens without float noise', () => {
    expect(fromUnits(0.3, 'team')).toBe(30_000_000);
    expect(toUnits(30_000_000, 'team')).toBe(0.3);
    expect(fromUnits(2, 'bot')).toBe(20_000_000);
    expect(toUnits(fromUnits(0.1 + 0.2, 'team'), 'team')).toBe(0.3);
  });

  it('writes a count the way people read it', () => {
    expect(formatTokens(120_000_000, 'zh-CN')).toBe('1.2 亿');
    expect(formatTokens(35_000_000, 'zh-CN')).toBe('3500 万');
    expect(formatTokens(950, 'zh-CN')).toBe('950');
    expect(formatTokens(120_000_000, 'en')).toBe('120M');
    expect(formatTokens(1_500, 'en')).toBe('1.5K');
  });
});

describe('the units of a development build', () => {
  it('counts in 万 so a limit can be reached by a few turns', () => {
    expect(scaleOf('development')).toBe('test');
    expect(scaleOf('production')).toBe('release');
    expect(unitSize('team', 'test')).toBe(10_000);
    expect(unitSize('bot', 'test')).toBe(10_000);
    // A team still starts above a member, as it does in a release build.
    expect(defaultTokens('team', 'test')).toBe(100_000);
    expect(defaultTokens('bot', 'test')).toBe(10_000);
    expect(toUnits(30_000, 'bot', 'test')).toBe(3);
    expect(fromUnits(2, 'team', 'test')).toBe(20_000);
  });

  it('names the unit by the build, not by anything the user sets', () => {
    expect(unitLabelKey('team')).toBe('settings.budgetUnitTeam');
    expect(unitLabelKey('bot', 'test')).toBe('settings.budgetUnitBotTest');
  });
});
