// The default daily budget in Settings. The scope and its limit are one
// setting: a scope without a limit, or a limit without a scope, is refused by
// the server, so choosing a scope has to send both.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Settings } from '../../api/types';
import '../../i18n';
import { CollaborationSection } from './SettingsSections';

const baseSettings = {
  revision: 1,
  language: 'zh-CN',
  colorMode: 'light',
  theme: 'classic',
  defaultModel: '',
  defaultProvider: '',
  memberLimit: 6,
  chainLimit: 10,
  reviewRejectLimit: 3,
  budgetScope: '',
  budgetTokens: 0,
  enginePath: '',
} as Settings;

describe('the default daily budget', () => {
  let root: Root;
  let host: HTMLDivElement;
  const save = vi.fn();

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    save.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = (settings: Settings) => act(() => root.render(h(CollaborationSection, { settings, save, errors: {} })));
  // The budget scope is the second select on the page after the limits card has none.
  const scopeSelect = () => host.querySelector('button[role="combobox"]') as HTMLButtonElement;
  const choose = (label: string) => {
    act(() => scopeSelect().click());
    act(() => ([...host.querySelectorAll('li[role="option"]')].find((li) => li.textContent?.includes(label)) as HTMLLIElement).click());
  };

  it('is unlimited by default and offers no limit field', () => {
    render(baseSettings);
    expect(scopeSelect().textContent).toContain('不限制');
    expect(host.textContent).not.toContain('默认每日上限');
  });

  it('starts a team at 1 亿 when teams are chosen, in one save with the scope', () => {
    render(baseSettings);
    choose('按团队');
    expect(save).toHaveBeenCalledWith('budgetScope', { budgetScope: 'team', budgetTokens: 100_000_000 }, expect.any(Function));
  });

  it('starts a member at 1 千万 when members are chosen', () => {
    render(baseSettings);
    choose('按成员');
    expect(save).toHaveBeenCalledWith('budgetScope', { budgetScope: 'bot', budgetTokens: 10_000_000 }, expect.any(Function));
  });

  it('shows the limit in the scope\'s own unit', () => {
    render({ ...baseSettings, budgetScope: 'team', budgetTokens: 300_000_000 });
    const inputs = host.querySelectorAll<HTMLInputElement>('input[type="number"]');
    const input = inputs[inputs.length - 1];
    expect(input.value).toBe('3');
    expect(input.step).toBe('1');
    expect(input.min).toBe('1');
    expect(host.textContent).toContain('亿 Token');
  });

  it('goes back to no limit with a zero limit, so the server accepts it', () => {
    render({ ...baseSettings, budgetScope: 'team', budgetTokens: 100_000_000 });
    choose('不限制');
    expect(save).toHaveBeenCalledWith('budgetScope', { budgetScope: '', budgetTokens: 0 }, expect.any(Function));
  });
});
