// The model picker in Settings follows the provider the profile runs with.
//
// A gateway offers its own models and not the engine's, and picking one
// writes that gateway's own default model: the profile's setting is the
// fallback for a profile that runs with no provider at all.
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
  engineModels: [] as unknown[],
  saveProvider: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('../../api/queries', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/queries')>();
  return {
    ...actual,
    useEngine: () => ({ data: { status: 'ready' } }),
    useEngineModels: () => ({ data: { models: state.engineModels } }),
    useProviders: () => ({ data: { providers: state.providers } }),
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
};

const engineModel = { id: 'gpt-6.1-sol', displayName: 'GPT 6.1' };

describe('the model picker follows the provider in use', () => {
  let root: Root;
  let host: HTMLDivElement;
  let save: Mock<Save>;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    save = vi.fn<Save>();
    state.providers = [];
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

  it('offers the models the gateway brought in, not the engine ones', () => {
    state.providers = [gateway];
    render({ defaultProvider: 'cpa' });
    open();
    const offered = options();
    expect(offered.some((text) => text.includes('DeepSeek V4 Pro'))).toBe(true);
    expect(offered.some((text) => text.includes('Grok 4.6'))).toBe(true);
    expect(offered.some((text) => text.includes('GPT 6.1'))).toBe(false);
  });

  it('marks a model that failed its check', () => {
    state.providers = [gateway];
    render({ defaultProvider: 'cpa' });
    open();
    expect(options().find((text) => text.includes('Grok 4.6'))).toContain('不可用');
    expect(options().find((text) => text.includes('DeepSeek V4 Pro'))).not.toContain('不可用');
  });

  it('writes the model to the gateway itself, not to the profile', () => {
    state.providers = [gateway];
    render({ defaultProvider: 'cpa' });
    open();
    pick('Grok 4.6');
    expect(state.saveProvider).toHaveBeenCalledWith(
      {
        id: 'cpa',
        body: expect.objectContaining({
          id: 'cpa',
          name: '公司的中转站',
          authMode: 'api_key',
          defaultModel: 'grok-4.6',
          expectedRevision: 4,
        }),
      },
      expect.anything(),
    );
    expect(save).not.toHaveBeenCalled();
  });

  it('keeps the engine models when no gateway is in use', () => {
    render();
    open();
    expect(options().some((text) => text.includes('GPT 6.1'))).toBe(true);
    pick('GPT 6.1');
    expect(save).toHaveBeenCalledWith('defaultModel', { defaultModel: 'gpt-6.1-sol' }, expect.anything());
    expect(state.saveProvider).not.toHaveBeenCalled();
  });

  it('keeps the engine models for the provider that uses the engine own account', () => {
    state.providers = [{ ...gateway, id: 'openai', authMode: 'chatgpt', models: [] }];
    render({ defaultProvider: 'openai' });
    open();
    expect(options().some((text) => text.includes('GPT 6.1'))).toBe(true);
  });

  it('shows the model a turn will ask for when the gateway named no default', () => {
    state.providers = [{ ...gateway, defaultModel: undefined }];
    render({ defaultProvider: 'cpa' });
    const chosen = host.querySelector('button[role="combobox"]')?.textContent ?? '';
    expect(chosen).toContain('DeepSeek V4 Pro');
  });
});
