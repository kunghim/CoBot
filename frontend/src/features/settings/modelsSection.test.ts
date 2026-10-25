// The default model in Settings: the one a member runs with until it is
// given its own in the conversation. Any enabled provider's model can be the
// default, and the choice is one write of the provider and the model
// together.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import type { Settings, SettingsPatch } from '../../api/types';
import '../../i18n';
import { ModelsSection } from './SettingsSections';

// The shape the settings page hands down: which field changed, the new
// value for it, and a way back when the server refuses the change.
type Save = (field: string, patch: SettingsPatch, rollback?: (persisted: Settings) => void) => void;

const state = vi.hoisted(() => ({
  providers: [] as unknown[],
  providersPending: false,
  enginePending: false,
  modelsPending: false,
  engineModels: [] as unknown[],
  saveProvider: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('../../api/queries', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/queries')>();
  return {
    ...actual,
    useEngine: () => ({ data: state.enginePending ? undefined : { status: 'ready', loggedIn: true }, isPending: state.enginePending }),
    useProviderPresets: () => ({ data: { presets: [] } }),
    useEngineModels: () => ({ data: state.modelsPending ? undefined : { models: state.engineModels }, isPending: state.modelsPending }),
    useProviders: () => ({ data: { providers: state.providers }, isPending: state.providersPending }),
    useSaveProvider: () => ({ mutate: state.saveProvider }),
  };
});
vi.mock('../../components/ui/Toast', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../components/ui/Toast')>();
  return { ...actual, useToast: () => ({ show: state.toast, dismiss: vi.fn() }) };
});

const gateway = {
  id: 'cpa',
  name: '公司的中转站',
  kind: 'custom',
  authMode: 'api_key',
  endpoints: [{ dialect: 'openai-responses', baseUrl: 'https://cpa.example.invalid/v1' }],
  models: [
    { id: 'deepseek-v4-pro', displayName: 'DeepSeek V4 Pro', status: 'ok' },
    { id: 'grok-4.6', displayName: 'Grok 4.6', status: 'failed' },
  ],
  defaultModel: 'deepseek-v4-pro',
  revision: 4,
  createdAt: '2026-10-20T00:00:00Z',
  updatedAt: '2026-10-20T00:00:00Z',
  hasCredential: true,
  enabled: true,
};

const official = { id: 'openai', name: 'OpenAI', kind: 'official', authMode: 'chatgpt', endpoints: [], models: [], enabled: true, revision: 1 };

const engineModel = { id: 'gpt-6.1-sol', displayName: 'GPT 6.1' };

describe('the default model can come from any enabled provider', () => {
  let root: Root;
  let host: HTMLDivElement;
  let save: Mock<Save>;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    save = vi.fn<Save>();
    state.providers = [official, gateway];
    state.providersPending = false;
    state.enginePending = false;
    state.modelsPending = false;
    state.engineModels = [engineModel];
    state.saveProvider.mockReset();
    state.toast.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = (over: Record<string, unknown> = {}) =>
    act(() =>
      root.render(
        h(ModelsSection, {
          settings: { defaultModel: 'engine-model', defaultProvider: '', ...over } as unknown as Settings,
          save,
          errors: {},
        }),
      ),
    );

  const open = () => act(() => (host.querySelector('button[role="combobox"]') as HTMLButtonElement).click());
  const options = () => [...host.querySelectorAll('li[role="option"]')].map((li) => li.textContent ?? '');
  const pick = (label: string) =>
    act(() =>
      ([...host.querySelectorAll('li[role="option"]')].find((li) => li.textContent?.includes(label)) as HTMLLIElement).click(),
    );

  it('offers the models of every enabled provider, named after their provider', () => {
    render();
    open();
    const listed = options();
    expect(listed.some((label) => label.includes('OpenAI') && label.includes('GPT 6.1'))).toBe(true);
    expect(listed.some((label) => label.includes('公司的中转站') && label.includes('DeepSeek V4 Pro'))).toBe(true);
  });

  it('marks a model that failed its check', () => {
    render();
    open();
    expect(options().find((label) => label.includes('Grok 4.6'))).toContain('不可用');
  });

  it('writes the provider and the model together', () => {
    render();
    open();
    pick('DeepSeek V4 Pro');
    expect(save).toHaveBeenCalledWith('defaultModel', { defaultProvider: 'cpa', defaultModel: 'deepseek-v4-pro' });
    expect(state.saveProvider).not.toHaveBeenCalled();
  });

  it('shows the default the settings name', () => {
    render({ defaultProvider: 'cpa', defaultModel: 'deepseek-v4-pro' });
    expect(host.querySelector('button[role="combobox"]')?.textContent).toContain('DeepSeek V4 Pro');
  });

  it('leaves a disabled provider out', () => {
    state.providers = [official, { ...gateway, enabled: false }];
    render();
    open();
    expect(options().some((label) => label.includes('DeepSeek V4 Pro'))).toBe(false);
  });

  it('waits for the provider list instead of showing the engine models first', () => {
    state.providersPending = true;
    render();
    expect(host.querySelector('button[role="combobox"]')).toBeNull();
  });

  // The official account's models come from the engine, so the page does not
  // know what is on offer, nor whether the account is signed in, until the
  // engine has answered; saying there is nothing to choose before that is a
  // wrong message that vanishes a moment later.
  it('waits for the engine instead of saying there is nothing to choose', () => {
    state.providers = [official];
    state.enginePending = true;
    render();
    expect(host.textContent).not.toContain('还没有可选的模型');
    expect(host.querySelector('button[role="combobox"]')).toBeNull();
  });

  it('waits for the engine models once the engine is ready', () => {
    state.providers = [official];
    state.modelsPending = true;
    render();
    expect(host.textContent).not.toContain('还没有可选的模型');
  });

  it('says what to do when no model is on offer', () => {
    state.providers = [{ ...official, enabled: false }];
    render();
    expect(host.textContent).toContain('还没有可选的模型');
  });
});
