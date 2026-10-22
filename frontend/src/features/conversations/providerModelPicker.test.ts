// The composer's model picker offers every enabled provider's models and
// writes the choice to the member.
//
// It used to follow one provider the whole profile ran with, so a member
// could not be moved to another vendor without moving every team, and the
// ChatGPT account's models disappeared as soon as a gateway was in use.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Bot, Team } from '../../api/types';
import ReasoningEffortSelect from './ReasoningEffortSelect';

const state = vi.hoisted(() => ({
  providers: [] as unknown[],
  defaultProvider: '',
  defaultModel: '',
  loggedIn: true,
  setModel: vi.fn(),
}));

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('../../api/queries', () => ({
  useSettings: () => ({ data: { defaultModel: state.defaultModel, defaultProvider: state.defaultProvider } }),
  useEngine: () => ({ data: { status: 'ready', loggedIn: state.loggedIn } }),
  useEngineModels: () => ({
    data: {
      models: [{
        id: 'gpt-6.1-sol', isDefault: true, defaultReasoningEffort: 'medium',
        reasoningEfforts: [{ effort: 'low' }, { effort: 'medium' }, { effort: 'high' }],
      }],
    },
  }),
  useProviders: () => ({ data: { providers: state.providers } }),
  useProviderPresets: () => ({ data: { presets: [] } }),
  useSetBotModel: () => ({ mutate: state.setModel }),
  useSetTeamReasoningEffort: () => ({ mutateAsync: vi.fn(), isPending: false }),
}));
vi.mock('../../components/ui/Toast', () => ({ useToast: () => ({ show: vi.fn(), dismiss: vi.fn() }) }));

const official = { id: 'openai', name: 'OpenAI', kind: 'official', authMode: 'chatgpt', endpoints: [], models: [], enabled: true, revision: 1 };
const gateway = {
  id: 'mimo',
  name: 'Xiaomi MiMo',
  kind: 'official',
  authMode: 'api_key',
  endpoints: [{ dialect: 'openai-responses', baseUrl: 'https://api.xiaomimimo.com/v1' }],
  models: [{ id: 'mimo-v2.6-pro' }, { id: 'mimo-v2.6-flash', status: 'failed' }],
  defaultModel: 'mimo-v2.6-pro',
  enabled: true,
  revision: 2,
};

describe('the composer offers the models of every enabled provider', () => {
  let root: Root;
  let host: HTMLDivElement;
  const team = { id: 'team-1', revision: 7, reasoningEffort: 'medium' } as Team;
  let bot: Bot;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    state.providers = [official, gateway];
    state.defaultProvider = '';
    state.defaultModel = '';
    state.loggedIn = true;
    state.setModel.mockReset();
    bot = { id: 'bot-1', name: 'engineer', revision: 3 } as Bot;
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = () => act(() => root.render(h(ReasoningEffortSelect, { team, bot })));
  const trigger = () => host.querySelector('button[aria-haspopup="dialog"]') as HTMLButtonElement;
  const panel = () => host.querySelector('[role="dialog"]') as HTMLElement;
  const openList = (current: string) => {
    act(() => trigger().click());
    const button = [...panel().querySelectorAll('button')].find((b) => b.textContent?.includes(current));
    act(() => button!.click());
  };
  const option = (text: string) =>
    [...host.querySelectorAll('li[role="option"] button')].find((b) => b.textContent?.includes(text)) as HTMLButtonElement | undefined;
  const headings = () => [...host.querySelectorAll('li[role="option"] p')].map((p) => p.textContent);

  it('lists the ChatGPT account and the gateway side by side, under their names', () => {
    render();
    openList('gpt-6.1-sol');
    expect(option('gpt-6.1-sol')).toBeTruthy();
    expect(option('mimo-v2.6-pro')).toBeTruthy();
    expect(headings()).toEqual(['OpenAI', 'Xiaomi MiMo']);
  });

  it('writes the choice to the member, provider and model together', () => {
    render();
    openList('gpt-6.1-sol');
    act(() => option('mimo-v2.6-pro')!.click());
    expect(state.setModel).toHaveBeenCalledTimes(1);
    const [vars] = state.setModel.mock.calls[0];
    expect(vars).toEqual({ botId: 'bot-1', providerId: 'mimo', model: 'mimo-v2.6-pro', expectedRevision: 3 });
  });

  it('shows the member its own choice', () => {
    bot = { ...bot, providerId: 'mimo', model: 'mimo-v2.6-pro' };
    render();
    expect(trigger().textContent).toContain('mimo-v2.6-pro');
  });

  it('follows the default model for a member that chose nothing', () => {
    state.defaultProvider = 'mimo';
    state.defaultModel = 'mimo-v2.6-pro';
    render();
    expect(trigger().textContent).toContain('mimo-v2.6-pro');
  });

  it('marks a model that failed its check and does not let it be chosen', () => {
    render();
    openList('gpt-6.1-sol');
    const failed = option('mimo-v2.6-flash');
    expect(failed?.textContent).toContain('settings.modelUnavailable');
    expect(failed?.disabled).toBe(true);
  });

  it('leaves a disabled provider out', () => {
    state.providers = [official, { ...gateway, enabled: false }];
    render();
    openList('gpt-6.1-sol');
    expect(option('mimo-v2.6-pro')).toBeUndefined();
  });

  it('offers no ChatGPT models until the account is signed in', () => {
    state.loggedIn = false;
    render();
    expect(trigger().textContent).toContain('mimo-v2.6-pro');
    openList('mimo-v2.6-pro');
    expect(option('gpt-6.1-sol')).toBeUndefined();
  });

  it('moves a member whose provider was disabled to the next choice', () => {
    bot = { ...bot, providerId: 'mimo', model: 'mimo-v2.6-pro' };
    state.providers = [official, { ...gateway, enabled: false }];
    render();
    expect(trigger().textContent).toContain('gpt-6.1-sol');
  });
});
