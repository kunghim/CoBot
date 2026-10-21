// The model provider settings: which provider is in use, how a provider is
// enabled, and which of the two checks runs when.
//
// The provider in use is the server's answer (settings.defaultProvider), and
// a check that runs without a key in the browser is the server's (POST
// /providers/{id}/probe) — the browser never holds a stored key.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { queryKeys } from '../../api/queries';
import '../../i18n';
import ProviderSection from './ProviderSection';

const state = vi.hoisted(() => ({
  providers: [] as unknown[],
  presets: [] as unknown[],
  settings: { revision: 3, defaultProvider: '' } as Record<string, unknown>,
  update: vi.fn(),
  save: vi.fn(),
  remove: vi.fn(),
  reprobe: vi.fn(),
  discover: vi.fn(),
  probe: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('../../api/queries', () => ({
  queryKeys: { settings: ['settings'] },
  useProviders: () => ({ data: { providers: state.providers }, isPending: false, isSuccess: true, isError: false }),
  useProviderPresets: () => ({ data: { presets: state.presets }, isPending: false, isSuccess: true, isError: false }),
  useSettings: () => ({ data: state.settings, isPending: false }),
  useUpdateSettings: () => ({ mutate: state.update, mutateAsync: state.update }),
  useDeleteProvider: () => ({ mutateAsync: state.remove }),
  useReprobeProvider: () => ({ mutateAsync: state.reprobe }),
  useSaveProvider: () => ({ mutateAsync: state.save }),
}));
vi.mock('../../api/client', () => ({
  api: {
    discoverProviderModels: (...args: unknown[]) => state.discover(...args),
    probeProviderModels: (...args: unknown[]) => state.probe(...args),
  },
}));
vi.mock('../../components/ui/Toast', () => ({ useToast: () => ({ show: state.toast, dismiss: vi.fn() }) }));

const preset = {
  id: 'deepseek',
  name: 'DeepSeek',
  short: 'DS',
  color: '#4D6BFE',
  keyUrl: 'platform.deepseek.com/api_keys',
  official: false,
  authMode: 'api_key',
  endpoints: [
    { dialect: 'openai-responses', baseUrl: 'api.deepseek.com/v1' },
    { dialect: 'anthropic-messages', baseUrl: 'api.deepseek.com/anthropic' },
  ],
  models: [{ id: 'deepseek-v4-pro' }],
  keyPatterns: ['^sk-'],
};

const provider = {
  id: 'deepseek',
  name: 'DeepSeek',
  kind: 'official',
  authMode: 'api_key',
  endpoints: [{ dialect: 'openai-responses', baseUrl: 'https://api.deepseek.com/v1' }],
  models: [{ id: 'deepseek-v4-pro', status: 'ok' }],
  defaultModel: 'deepseek-v4-pro',
  revision: 3,
  createdAt: '2026-10-20T00:00:00Z',
  updatedAt: '2026-10-20T00:00:00Z',
  hasCredential: true,
  presetId: 'deepseek',
};

describe('provider settings', () => {
  let root: Root;
  let host: HTMLDivElement;
  let client: QueryClient;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    client = new QueryClient();
    state.providers = [];
    state.presets = [preset];
    state.settings = { revision: 3, defaultProvider: '' };
    client.setQueryData(queryKeys.settings, state.settings);
    state.update.mockReset().mockImplementation((_vars, callbacks) => callbacks?.onSuccess?.(state.settings));
    state.save.mockReset().mockResolvedValue(provider);
    state.reprobe.mockReset().mockResolvedValue({ results: { 'deepseek-v4-pro': { ok: true } } });
    state.discover.mockReset().mockResolvedValue({ models: ['deepseek-v4-pro', 'deepseek-flash'] });
    state.probe.mockReset().mockResolvedValue({ results: { 'deepseek-v4-pro': { ok: true } } });
    state.toast.mockReset();
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = () => act(() => root.render(h(QueryClientProvider, { client }, h(ProviderSection, {}))));

  const byText = (text: string) =>
    [...host.querySelectorAll('button')].find((button) => button.textContent?.includes(text));

  const setValue = (input: HTMLInputElement, value: string) => {
    const setter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value')!.set!;
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  };

  it('shows the provider the server names as the one in use', () => {
    state.providers = [provider];
    state.settings = { revision: 3, defaultProvider: 'deepseek' };
    render();
    const groups = host.querySelectorAll('p');
    const inUse = [...groups].find((node) => node.textContent === '使用中');
    expect(inUse?.nextElementSibling?.textContent).toContain('DeepSeek');
    expect(inUse?.nextElementSibling?.textContent).toContain('https://api.deepseek.com/v1');
    expect(inUse?.nextElementSibling?.textContent).toContain('deepseek-v4-pro');
  });

  it('enables another provider by writing the profile default', () => {
    state.providers = [provider];
    state.settings = { revision: 3, defaultProvider: '' };
    render();
    act(() => byText('启用')!.click());
    expect(state.update).toHaveBeenCalledWith(
      { patch: { defaultProvider: 'deepseek' }, expectedRevision: 3 },
      expect.anything(),
    );
  });

  it('makes the first provider the one in use right after it is added', async () => {
    render();
    act(() => byText('添加供应商')!.click());
    act(() => byText('DeepSeek')!.click());
    const key = host.querySelector('input[type="password"]') as HTMLInputElement;
    act(() => setValue(key, 'sk-test'));
    await act(async () => {
      byText('获取可用模型')!.click();
    });
    const chip = [...host.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')].find((button) => button.textContent?.includes('deepseek-v4-pro'));
    act(() => chip!.click());
    await act(async () => {
      byText('测试并添加')!.click();
    });
    await act(async () => {
      byText('保存')!.click();
    });
    expect(state.update).toHaveBeenCalledWith(
      { patch: { defaultProvider: 'deepseek' }, expectedRevision: 3 },
      expect.anything(),
    );
    expect(state.toast).toHaveBeenCalledWith(expect.objectContaining({ title: '已添加并启用 DeepSeek，纳入 1 个模型' }));
  });

  it('re-checks a stored provider on the server when the key is left alone', async () => {
    state.providers = [provider];
    state.settings = { revision: 3, defaultProvider: 'deepseek' };
    render();
    act(() => byText('编辑')!.click());
    await act(async () => {
      byText('测试并保存')!.click();
    });
    expect(state.reprobe).toHaveBeenCalledWith('deepseek');
    expect(state.probe).not.toHaveBeenCalled();
    await act(async () => {
      byText('保存')!.click();
    });
    const body = state.save.mock.calls[0][0].body;
    expect(body.apiKey).toBeUndefined();
    expect(body.models).toEqual([{ id: 'deepseek-v4-pro', status: 'ok' }]);
    expect(body.expectedRevision).toBe(3);
  });

  it('clears the profile default when the provider in use is removed', async () => {
    state.providers = [provider];
    state.settings = { revision: 3, defaultProvider: 'deepseek' };
    state.remove.mockReset().mockResolvedValue({ deleted: true });
    render();
    act(() => byText('移除')!.click());
    const confirm = [...host.querySelectorAll<HTMLButtonElement>('[role="alertdialog"] button')].find((button) => button.textContent?.includes('移除'));
    await act(async () => {
      confirm!.click();
    });
    expect(state.remove).toHaveBeenCalledWith('deepseek');
    expect(state.update).toHaveBeenCalledWith({ patch: { defaultProvider: '' }, expectedRevision: 3 });
  });

  it('sends the official vendor to the engine section instead of a form', () => {
    state.presets = [{ ...preset, id: 'openai', name: 'OpenAI', short: 'OA', official: true, authMode: 'chatgpt', endpoints: [], models: [{ id: 'gpt-6.1-sol' }] }];
    render();
    act(() => byText('添加供应商')!.click());
    act(() => byText('OpenAI')!.click());
    expect(state.toast).toHaveBeenCalledWith(expect.objectContaining({ title: '请在「Codex 引擎」分区登录 ChatGPT 账号' }));
    expect(host.querySelector('[role="dialog"]')).toBeFalsy();
  });
});
