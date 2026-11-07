import { describe, expect, it } from 'vitest';
import en from './locales/en.json';
import zhCN from './locales/zh-CN.json';

// The window's own close button quits CoBot. While a dialog is open it is
// still in the accessibility tree, so a tool or a screen reader that finds a
// button by its name must not mistake it for the dialog's close button.
describe('window close label', () => {
  it.each([['en', en], ['zh-CN', zhCN]])('%s names quitting apart from closing a dialog', (_, locale) => {
    expect(locale.window.close).not.toBe(locale.common.close);
    expect(locale.window.close).not.toBe(locale.common.cancel);
  });
});
