// The daily token budget is written in the unit that fits what it limits: a
// team spends a lot more than one member, so a team's limit is counted in 亿
// (100 million tokens) and a member's in 千万 (10 million).
//
// A development build counts in 万 (10 thousand) instead, so a limit can be
// reached with a few turns of a test. Which one applies comes from the build,
// never from the user.

export type BudgetKind = 'team' | 'bot';

/** release: what users see; test: the development build's small units. */
export type BudgetScale = 'release' | 'test';

const UNIT_SIZE: Record<BudgetScale, Record<BudgetKind, number>> = {
  release: { team: 100_000_000, bot: 10_000_000 },
  test: { team: 10_000, bot: 10_000 },
};

/** How many units a freshly chosen limit starts with. */
const START_UNITS: Record<BudgetScale, Record<BudgetKind, number>> = {
  release: { team: 1, bot: 1 },
  test: { team: 10, bot: 1 },
};

/** The scale a build uses: development builds count small. */
export function scaleOf(profile: string | undefined): BudgetScale {
  return profile === 'development' ? 'test' : 'release';
}

/** Tokens in one unit of the kind. */
export function unitSize(kind: BudgetKind, scale: BudgetScale = 'release'): number {
  return UNIT_SIZE[scale][kind];
}

/** The limit a freshly chosen kind starts with, in tokens. */
export function defaultTokens(kind: BudgetKind, scale: BudgetScale = 'release'): number {
  return UNIT_SIZE[scale][kind] * START_UNITS[scale][kind];
}

/** How many units a number of tokens is. */
export function toUnits(tokens: number, kind: BudgetKind, scale: BudgetScale = 'release'): number {
  return Math.round((tokens / UNIT_SIZE[scale][kind]) * 1000) / 1000;
}

/** How many tokens a number of units is, in whole tokens. */
export function fromUnits(units: number, kind: BudgetKind, scale: BudgetScale = 'release'): number {
  return Math.round(units * UNIT_SIZE[scale][kind]);
}

/** One step of the number field, in units. */
export function stepOf(_kind: BudgetKind): number {
  return 1;
}

/** The smallest limit worth setting: one unit. */
export function minUnits(_kind: BudgetKind): number {
  return 1;
}

/** The name of the unit's text in the language files. */
export function unitLabelKey(kind: BudgetKind, scale: BudgetScale = 'release'): string {
  return `settings.budgetUnit${kind === 'team' ? 'Team' : 'Bot'}${scale === 'test' ? 'Test' : ''}`;
}

/**
 * A count of tokens as people read it: 1.2 亿, 3500 万 in Chinese; 120M,
 * 35M in English.
 */
export function formatTokens(tokens: number, language: string): string {
  const chinese = language.startsWith('zh');
  const trim = (value: number) => String(Math.round(value * 100) / 100);
  if (chinese) {
    if (tokens >= 100_000_000) return `${trim(tokens / 100_000_000)} 亿`;
    if (tokens >= 10_000) return `${trim(tokens / 10_000)} 万`;
    return String(tokens);
  }
  if (tokens >= 1_000_000_000) return `${trim(tokens / 1_000_000_000)}B`;
  if (tokens >= 1_000_000) return `${trim(tokens / 1_000_000)}M`;
  if (tokens >= 1_000) return `${trim(tokens / 1_000)}K`;
  return String(tokens);
}
