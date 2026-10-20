// The composer's model picker follows the provider the profile runs with.
//
// It used to list the engine's own models and write the profile's default
// model, so a gateway's models could not be picked in a conversation — and
// what it wrote was a setting the core ignores when a provider is in use.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Team } from '../../api/types';
import ReasoningEffortSelect from './ReasoningEffortSelect';

const state = vi.hoisted(() => ({
  providers: [] as unknown[],
  defaultProvider: '',
  saveProvider: vi.fn(),
  saveSetting: vi.fn(),
}));

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('../../api/queries', () => ({
  useSettings: () => ({ data: { defaultModel: '', defaultProvider: state.defaultProvider } }),
  useEngineModels: () => ({
    data: {
      models: [{
        id: 'gpt-6.1-sol', isDefault: true, defaultReasoningEffort: 'medium',
        reasoningEfforts: [{ effort: 'low' }, { effort: 'medium' }, { effort: 'high' }],
      }],
    },
  }),
  useProviders: () => ({ data: { providers: state.providers } }),
  useSaveProvider: () => ({ mutate: state.saveProvider }),
  useSetTeamReasoningEffort: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock('../../components/ui/Toast', () => ({ useToast: () => ({ show: vi.fn(), dismiss: vi.fn() }) }));
vi.mock('../settings/useSettingSaver', () => ({ useSettingSaver: () => ({ save: state.saveSetting }) }));

const gateway = {
  id: 'mimo',
  name: 'Xiaomi MiMo',
  kind: 'official',
  authMode: 'api_key',
  endpoints: [
    { dialect: 'openai-responses', baseUrl: 'https://api.xiaomimimo.com/v1' },
    { dialect: 'anthropic-messages', baseUrl: 'https://api.xiaomimimo.com/anthropic' },
  ],
  models: [{ id: 'mimo-v2.6-pro' }, { id: 'mimo-v2.6-flash' }],
  defaultModel: 'mimo-v2.6-pro',
  revision: 2,
};

describe('the composer picks the models of the provider in use', () => {
  let root: Root;
  let host: HTMLDivElement;
  const team = { id: 'team-1', revision: 7, reasoningEffort: 'medium' } as Team;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    state.providers = [];
    state.defaultProvider = '';
    state.saveProvider.mockReset();
    state.saveSetting.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = () => act(() => root.render(h(ReasoningEffortSelect, { team })));
  const trigger = () => host.querySelector('button[aria-haspopup="dialog"]') as HTMLButtonElement;
  const panel = () => host.querySelector('[role="dialog"]') as HTMLElement;
  const panelButton = (text: string) =>
    [...panel().querySelectorAll('button')].find((b) => b.textContent?.includes(text)) as HTMLButtonElement | undefined;
  const option = (text: string) =>
    [...host.querySelectorAll('li[role="option"] button')].find((b) => b.textContent?.includes(text)) as HTMLButtonElement | undefined;

  it('offers the gateway models and writes the gateway default when one is in use', () => {
    state.providers = [gateway];
    state.defaultProvider = 'mimo';
    render();
    // The trigger names the gateway's model, not the engine's.
    expect(trigger().textContent).toContain('mimo-v2.6-pro');
    act(() => trigger().click());
    // Open the model list inside the panel.
    const openList = panelButton('mimo-v2.6-pro');
    expect(openList).toBeTruthy();
    act(() => openList!.click());
    const pick = option('mimo-v2.6-flash');
    expect(pick).toBeTruthy();
    act(() => pick!.click());
    expect(state.saveProvider).toHaveBeenCalledTimes(1);
    const [vars] = state.saveProvider.mock.calls[0];
    expect(vars.id).toBe('mimo');
    expect(vars.body.defaultModel).toBe('mimo-v2.6-flash');
    expect(vars.body.expectedRevision).toBe(2);
    // The profile's own setting is not what a turn reads.
    expect(state.saveSetting).not.toHaveBeenCalled();
  });

  it('keeps the engine models when no gateway is in use', () => {
    render();
    expect(trigger().textContent).toContain('gpt-6.1-sol');
    act(() => trigger().click());
    const openList = panelButton('gpt-6.1-sol');
    act(() => openList!.click());
    expect(option('gpt-6.1-sol')).toBeTruthy();
    expect(state.saveProvider).not.toHaveBeenCalled();
  });
});
