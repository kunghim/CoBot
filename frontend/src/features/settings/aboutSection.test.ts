import { describe, expect, it } from 'vitest';
import { releaseStage, updateMessageKey } from './AboutSection';

describe('the about page', () => {
  it('names the stage of a published beta from its version number', () => {
    expect(releaseStage('0.1.0-beta.2')).toBe('beta');
    expect(releaseStage('0.1.0-alpha')).toBe('alpha');
    expect(releaseStage('0.1.0')).toBeNull();
    expect(releaseStage('0.1.0-beta.2-dev')).toBe('beta');
    expect(releaseStage(undefined)).toBeNull();
  });

  it('has a sentence for every answer an update check can give', () => {
    for (const state of ['upToDate', 'available', 'unknown', 'unreachable', 'rejected'] as const) {
      expect(updateMessageKey({ state, current: '0.1.0', checkedAt: '2026-10-24T00:00:00Z' })).toBe(`about.update.${state}`);
    }
  });
});
