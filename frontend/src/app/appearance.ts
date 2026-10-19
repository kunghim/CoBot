// Applies the user's appearance settings to the page: data-mode (light or
// dark, with "system" resolved through the OS preference) and data-theme
// (the palette) on <html>. The settings live in CoBot's settings; the last
// applied value is also kept in this browser so the next load paints in the
// right colors before the settings arrive.
import { useEffect } from 'react';
import type { Settings } from '../api/types';

export type ColorMode = Settings['colorMode'];
export type Theme = Settings['theme'];

export interface Appearance {
  colorMode: ColorMode;
  theme: Theme;
}

const storageKey = 'cobot.appearance';
const darkQuery = '(prefers-color-scheme: dark)';

export function resolveMode(mode: ColorMode, systemDark: boolean): 'light' | 'dark' {
  if (mode === 'system') return systemDark ? 'dark' : 'light';
  return mode;
}

function systemPrefersDark(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia(darkQuery).matches;
}

export function applyAppearance(appearance: Appearance, root: HTMLElement = document.documentElement): void {
  const mode = resolveMode(appearance.colorMode, systemPrefersDark());
  root.dataset.mode = mode;
  root.dataset.theme = appearance.theme;
  root.style.colorScheme = mode;
}

/** Keeps <html> in step with the settings and, in system mode, the OS. */
export function useAppearance(appearance: Appearance | undefined): void {
  const colorMode = appearance?.colorMode;
  const theme = appearance?.theme;
  useEffect(() => {
    if (!colorMode || !theme) return;
    const current = { colorMode, theme };
    applyAppearance(current);
    try {
      window.localStorage.setItem(storageKey, JSON.stringify(current));
    } catch {
      /* storage unavailable: the next load starts from the defaults */
    }
    if (colorMode !== 'system' || typeof window.matchMedia !== 'function') return;
    const query = window.matchMedia(darkQuery);
    const follow = () => applyAppearance(current);
    query.addEventListener('change', follow);
    return () => query.removeEventListener('change', follow);
  }, [colorMode, theme]);
}

/** Paints the last known appearance (or the defaults) before React renders. */
export function applyStoredAppearance(): void {
  let stored: Partial<Appearance> = {};
  try {
    stored = JSON.parse(window.localStorage.getItem(storageKey) ?? '{}') as Partial<Appearance>;
  } catch {
    stored = {};
  }
  const colorMode: ColorMode = stored.colorMode === 'light' || stored.colorMode === 'dark' ? stored.colorMode : 'system';
  const theme: Theme = stored.theme === 'harbor' || stored.theme === 'dusk' || stored.theme === 'classic' ? stored.theme : 'graphite';
  applyAppearance({ colorMode, theme });
}
