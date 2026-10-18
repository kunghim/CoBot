import { describe, expect, it } from 'vitest';
import { applyAppearance, resolveMode } from './appearance';

describe('appearance', () => {
  it('resolves system mode through the OS preference', () => {
    expect(resolveMode('system', true)).toBe('dark');
    expect(resolveMode('system', false)).toBe('light');
    expect(resolveMode('light', true)).toBe('light');
    expect(resolveMode('dark', false)).toBe('dark');
  });

  it('marks the root with the mode and the theme', () => {
    const root = document.createElement('html');
    applyAppearance({ colorMode: 'dark', theme: 'harbor' }, root);
    expect(root.dataset.mode).toBe('dark');
    expect(root.dataset.theme).toBe('harbor');
    expect(root.style.colorScheme).toBe('dark');
  });
});
