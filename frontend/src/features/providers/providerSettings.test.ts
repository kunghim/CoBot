// The model provider settings: every provider listed with its own switch,
// the ChatGPT account always among them, and which of the two checks runs
// when.
//
// A check that runs without a key in the browser is the server's (POST
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
  setEnabled: vi.fn(),
  save: vi.fn(),
  remove: vi.fn(),
  reprobe: vi.fn(),
  discover: vi.fn(),
  probe: vi.fn(),
  resolve: vi.fn(),
  stale: 0,
  apply: vi.fn(),
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
  useSetProviderEnabled: () => ({ mutate: state.setEnabled }),
  useModelCatalog: () => ({ data: { source: 'bundled', models: 227, updatedAt: '2026-10-24T00:00:00Z', engine: { active: state.stale > 0, stale: state.stale } } }),
  useUpdateModelCatalog: () => ({ mutate: vi.fn(), isPending: false, data: undefined }),
  useApplyEngineCatalog: () => ({ mutate: state.apply, isPending: false }),
  // The ChatGPT account's card drives its own sign-in.
  useEngine: () => ({ data: { status: 'ready', loggedIn: false }, refetch: vi.fn() }),
  useEngineLogin: () => ({ mutate: vi.fn(), isPending: false }),
  useEngineLoginCancel: () => ({ mutate: vi.fn(), isPending: false }),
  useEngineLogout: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock('../../api/client', () => ({
  api: {
    discoverProviderModels: (...args: unknown[]) => state.discover(...args),
    probeProviderModels: (...args: unknown[]) => state.probe(...args),
    resolveModels: (...args: unknown[]) => state.resolve(...args),
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
  enabled: true,
};

const official = {
  id: 'openai',
  name: 'OpenAI',
  kind: 'official',
  authMode: 'chatgpt',
  endpoints: [],
  models: [],
  revision: 1,
  createdAt: '1970-01-01T00:00:00Z',
  updatedAt: '1970-01-01T00:00:00Z',
  presetId: 'openai',
  enabled: true,
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
    state.setEnabled.mockReset();
    state.save.mockReset().mockResolvedValue(provider);
    state.reprobe.mockReset().mockResolvedValue({ results: { 'deepseek-v4-pro': { ok: true } } });
    state.discover.mockReset().mockResolvedValue({ models: ['deepseek-v4-pro', 'deepseek-flash'] });
    state.probe.mockReset().mockResolvedValue({ results: { 'deepseek-v4-pro': { ok: true } } });
    state.toast.mockReset();
    state.stale = 0;
    state.apply.mockReset();
    // Nothing is known about a model unless a test says so.
    state.resolve.mockReset().mockImplementation(async (models: Array<{ id: string; displayName?: string; contextWindow?: number; maxOutput?: number; inputModalities?: string[]; supportedEfforts?: string[]; thinkingToggle?: boolean; webSearch?: boolean }>) => ({
      specs: Object.fromEntries(
        models.map((model) => [
          model.id,
          {
            displayName: model.displayName || model.id,
            alias: model.displayName,
            contextWindow: model.contextWindow ?? 0,
            maxOutput: model.maxOutput ?? 0,
            contextLimit: 0,
            maxOutputLimit: 0,
            inputOptions: ['text', 'image', 'video', 'pdf'],
            inputModalities: ['text', ...(model.inputModalities ?? [])],
            webSearch: model.webSearch ?? false,
            thinking: { toggle: model.thinkingToggle ?? false, efforts: model.supportedEfforts?.length || model.thinkingToggle ? (model.supportedEfforts ?? []) : ['low', 'medium', 'high'] },
            source: 'custom',
            locked: [],
          },
        ]),
      ),
    }));
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

  it('lists the ChatGPT account first, then every provider with its own switch', () => {
    state.providers = [provider, official];
    render();
    const switches = [...host.querySelectorAll<HTMLButtonElement>('button[role="switch"]')];
    expect(switches).toHaveLength(2);
    expect(switches.every((entry) => entry.getAttribute('aria-checked') === 'true')).toBe(true);
    const text = host.textContent ?? '';
    expect(text.indexOf('OpenAI')).toBeLessThan(text.indexOf('DeepSeek'));
    expect(text).toContain('https://api.deepseek.com/v1');
    // Nothing reads as a single provider "in use" any more.
    expect(text).not.toContain('使用中');
  });

  it('switches a provider off without touching the profile settings', () => {
    state.providers = [official, provider];
    render();
    const deepseek = [...host.querySelectorAll<HTMLButtonElement>('button[role="switch"]')][1];
    act(() => deepseek.click());
    expect(state.setEnabled).toHaveBeenCalledWith({ id: 'deepseek', enabled: false, expectedRevision: 3 }, expect.anything());
    expect(state.update).not.toHaveBeenCalled();
  });

  it('shows a disabled provider as disabled', () => {
    state.providers = [official, { ...provider, enabled: false }];
    render();
    expect(host.textContent).toContain('已停用');
    const deepseek = [...host.querySelectorAll<HTMLButtonElement>('button[role="switch"]')][1];
    expect(deepseek.getAttribute('aria-checked')).toBe('false');
  });

  it('signs the ChatGPT account in on its own card and never offers to edit or remove it', () => {
    state.providers = [official];
    render();
    expect(byText('登录')).toBeTruthy();
    expect(byText('编辑')).toBeUndefined();
    expect(byText('移除')).toBeUndefined();
  });

  it('adds a provider without making it a profile default', async () => {
    state.providers = [official];
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
    // One that answers is enough: the provider is added without a second click.
    await act(async () => {
      byText('测试并添加')!.click();
    });
    expect(state.save).toHaveBeenCalledTimes(1);
    expect(state.update).not.toHaveBeenCalled();
    expect(state.toast).toHaveBeenCalledWith(expect.objectContaining({ title: '已添加 DeepSeek，纳入 1 个模型' }));
  });

  it('keeps the ticked models when the user goes back and into the same vendor', async () => {
    state.providers = [official];
    render();
    act(() => byText('添加供应商')!.click());
    act(() => byText('DeepSeek')!.click());
    const key = host.querySelector('input[type="password"]') as HTMLInputElement;
    act(() => setValue(key, 'sk-test'));
    await act(async () => {
      byText('获取可用模型')!.click();
    });
    // The model chips; the thinking levels below them are chips too.
    const pressed = () =>
      [...host.querySelectorAll<HTMLButtonElement>('button[aria-pressed="true"]')]
        .map((button) => button.textContent?.replace('✓', '') ?? '')
        .filter((text) => text.startsWith('deepseek'));
    const chip = [...host.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')].find((button) => button.textContent?.includes('deepseek-v4-pro'));
    act(() => chip!.click());
    expect(pressed()).toEqual(['deepseek-v4-pro']);
    act(() => byText('返回')!.click());
    act(() => byText('DeepSeek')!.click());
    expect(pressed()).toEqual(['deepseek-v4-pro']);
  });

  it('finds models by search and keeps an unlisted one as typed', async () => {
    state.providers = [official];
    render();
    act(() => byText('添加供应商')!.click());
    act(() => byText('DeepSeek')!.click());
    const key = host.querySelector('input[type="password"]') as HTMLInputElement;
    act(() => setValue(key, 'sk-test'));
    await act(async () => {
      byText('获取可用模型')!.click();
    });
    // The model chips; the thinking levels below them are chips too.
    const chips = () =>
      [...host.querySelectorAll<HTMLButtonElement>('button[aria-pressed]')]
        .map((button) => button.textContent?.replace('✓', '') ?? '')
        .filter((text) => text.startsWith('deepseek'));
    expect(chips()).toEqual(['deepseek-flash', 'deepseek-v4-pro']);
    const search = host.querySelector('input[type="search"]') as HTMLInputElement;
    act(() => setValue(search, 'v4'));
    expect(chips()).toEqual(['deepseek-v4-pro']);
    act(() => setValue(search, 'deepseek-r9'));
    expect(chips()).toEqual([]);
    act(() => byText('添加「deepseek-r9」')!.click());
    expect(chips()).toEqual(['deepseek-flash', 'deepseek-r9', 'deepseek-v4-pro']);
  });

  it('saves a stored provider without asking again about models that already passed', async () => {
    state.providers = [official, provider];
    render();
    act(() => byText('编辑')!.click());
    await act(async () => {
      byText('获取可用模型')!.click();
    });
    expect(state.discover).toHaveBeenCalledWith('https://api.deepseek.com/v1', undefined, 'deepseek');
    await act(async () => {
      byText('测试并保存')!.click();
    });
    expect(state.probe).not.toHaveBeenCalled();
    expect(state.reprobe).not.toHaveBeenCalled();
    const body = state.save.mock.calls[0][0].body;
    expect(body.apiKey).toBeUndefined();
    expect(body.models).toEqual([{ id: 'deepseek-v4-pro', status: 'ok' }]);
    expect(body.expectedRevision).toBe(3);
  });

  it('asks again about every model once the key is changed', async () => {
    state.providers = [official, provider];
    render();
    act(() => byText('编辑')!.click());
    const key = host.querySelector('input[type="password"]') as HTMLInputElement;
    act(() => setValue(key, 'sk-other'));
    await act(async () => {
      byText('测试并保存')!.click();
    });
    expect(state.probe).toHaveBeenCalledWith('https://api.deepseek.com/v1', ['deepseek-v4-pro'], 'sk-other', 'deepseek');
    expect(state.save).toHaveBeenCalledTimes(1);
  });

  it('ticks every model the search shows with one button, and clears them again', async () => {
    state.providers = [official];
    render();
    act(() => byText('添加供应商')!.click());
    act(() => byText('DeepSeek')!.click());
    const key = host.querySelector('input[type="password"]') as HTMLInputElement;
    act(() => setValue(key, 'sk-test'));
    await act(async () => {
      byText('获取可用模型')!.click();
    });
    const pressed = () =>
      [...host.querySelectorAll<HTMLButtonElement>('button[aria-pressed="true"]')].filter((button) => button.textContent?.includes('deepseek')).length;
    act(() => byText('全选')!.click());
    expect(pressed()).toBe(2);
    act(() => byText('清空')!.click());
    expect(pressed()).toBe(0);
  });

  it('warns about a missing OpenAI endpoint only once a Claude one is given', () => {
    state.providers = [official];
    render();
    act(() => byText('添加供应商')!.click());
    act(() => byText('自建网关 / 中转站')!.click());
    expect(host.textContent).not.toContain('没有填 OpenAI 格式端点');
    const claude = [...host.querySelectorAll<HTMLInputElement>('input')].find((input) => input.placeholder.includes('anthropic'))!;
    act(() => setValue(claude, 'https://gateway.example/anthropic'));
    expect(host.textContent).toContain('没有填 OpenAI 格式端点');
    expect(host.textContent).not.toContain('providers.');
  });

  // A new DeepSeek provider with both of its models ticked and a key typed.
  const startWithBothModels = async () => {
    state.providers = [official];
    render();
    act(() => byText('添加供应商')!.click());
    act(() => byText('DeepSeek')!.click());
    const key = host.querySelector('input[type="password"]') as HTMLInputElement;
    act(() => setValue(key, 'sk-test'));
    await act(async () => {
      byText('获取可用模型')!.click();
    });
    act(() => byText('全选')!.click());
  };

  it('does not add a provider on its own when some models did not answer', async () => {
    state.probe.mockResolvedValue({
      results: { 'deepseek-flash': { ok: false, code: 'model_unknown' }, 'deepseek-v4-pro': { ok: true } },
    });
    await startWithBothModels();
    await act(async () => {
      byText('测试并添加')!.click();
    });
    expect(state.save).not.toHaveBeenCalled();
    expect(host.textContent).toContain('1 个模型不可用');
    // Saving is then the user's own press, and asks nothing again.
    await act(async () => {
      byText('保存')!.click();
    });
    expect(state.probe).toHaveBeenCalledTimes(1);
    expect(state.save).toHaveBeenCalledTimes(1);
    expect(state.save.mock.calls[0][0].body.models).toEqual(
      expect.arrayContaining([expect.objectContaining({ id: 'deepseek-v4-pro', status: 'ok' }), expect.objectContaining({ id: 'deepseek-flash', status: 'failed' })]),
    );
  });

  it('checks one model from its own button and does not ask about it again', async () => {
    state.probe.mockResolvedValueOnce({
      results: { 'deepseek-flash': { ok: false, code: 'model_unknown' }, 'deepseek-v4-pro': { ok: true } },
    });
    await startWithBothModels();
    await act(async () => {
      byText('测试并添加')!.click();
    });
    expect(state.save).not.toHaveBeenCalled();
    // The failed one is tried again on its own...
    state.probe.mockResolvedValueOnce({ results: { 'deepseek-flash': { ok: true } } });
    // A model that failed offers retry, not test.
    expect(host.querySelector('button[aria-label="测试 deepseek-flash"]')).toBeNull();
    const retest = host.querySelector<HTMLButtonElement>('button[aria-label="重试 deepseek-flash"]')!;
    await act(async () => {
      retest.click();
    });
    expect(state.toast).toHaveBeenCalledWith(expect.objectContaining({ tone: 'success', title: 'deepseek-flash 可用' }));
    expect(state.probe).toHaveBeenLastCalledWith('https://api.deepseek.com/v1', ['deepseek-flash'], 'sk-test', undefined);
    // ...and with both answered, adding asks nothing more.
    await act(async () => {
      byText('测试并添加')!.click();
    });
    expect(state.probe).toHaveBeenCalledTimes(2);
    expect(state.save).toHaveBeenCalledTimes(1);
  });

  it('says in a toast how a single check ended, with the reason when it failed', async () => {
    await startWithBothModels();
    state.probe.mockResolvedValueOnce({ results: { 'deepseek-flash': { ok: false, code: 'auth_invalid' } } });
    await act(async () => {
      host.querySelector<HTMLButtonElement>('button[aria-label="测试 deepseek-flash"]')!.click();
    });
    expect(state.toast).toHaveBeenCalledWith(expect.objectContaining({ tone: 'warning', duration: 5000, title: 'deepseek-flash 不可用', description: '密钥无效' }));
    // The chip carries a mark, not the reason written after the name.
    expect(host.querySelector('[aria-label="不可用：密钥无效"]')).toBeTruthy();
    expect(host.textContent).not.toContain('不可用 · 密钥无效');
  });

  const bothAnswer = () => state.probe.mockResolvedValue({ results: { 'deepseek-flash': { ok: true }, 'deepseek-v4-pro': { ok: true } } });

  // The core answers what it knows about the kept models a moment after they are ticked.
  const openAdvanced = async () => {
    act(() => byText('高级设置')!.click());
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 300));
    });
  };
  const inputOf = (label: string) => {
    const wrapper = [...host.querySelectorAll('label')].find((entry) => entry.textContent?.includes(label));
    return (wrapper?.htmlFor ? host.querySelector(`[id="${wrapper.htmlFor}"]`) : null) as HTMLInputElement | null;
  };

  it('offers to restart the engines that still run an older model catalog', () => {
    state.providers = [official];
    render();
    expect(host.textContent).not.toContain('旧的目录');
    state.stale = 2;
    render();
    expect(host.textContent).toContain('有 2 个团队的引擎还在用旧的目录');
    act(() => byText('重启空闲的引擎')!.click());
    expect(state.apply).toHaveBeenCalled();
  });

  it('keeps advanced settings collapsed until they are asked for', async () => {
    await startWithBothModels();
    expect(host.textContent).not.toContain('最大输出长度');
    await openAdvanced();
    expect(host.textContent).toContain('最大输出长度');
  });

  it('keeps what the user enters for a model the catalog does not know', async () => {
    bothAnswer();
    await startWithBothModels();
    await openAdvanced();
    act(() => setValue(inputOf('别名')!, '闪电'));
    act(() => setValue(inputOf('上下文长度')!, '2000000'));
    act(() => byText('图片')!.click());
    act(() => (inputOf('可以关闭思考') as unknown as HTMLButtonElement).click());
    await act(async () => {
      byText('测试并添加')!.click();
    });
    const models = state.save.mock.calls[0][0].body.models as Array<Record<string, unknown>>;
    const flash = models.find((model) => model.id === 'deepseek-flash')!;
    expect(flash).toMatchObject({ displayName: '闪电', contextWindow: 2000000, inputModalities: ['text', 'image'], thinkingToggle: true });
    // The other model was left alone.
    expect(models.find((model) => model.id === 'deepseek-v4-pro')).toEqual({ id: 'deepseek-v4-pro', status: 'ok' });
  });

  it('treats what the catalog gives as a limit the user may lower but not pass', async () => {
    state.resolve.mockImplementation(async (models: Array<{ id: string; displayName?: string; contextWindow?: number }>) => ({
      specs: Object.fromEntries(
        models.map((model) => [
          model.id,
          {
            displayName: model.displayName || 'DeepSeek V4',
            name: 'DeepSeek V4',
            contextWindow: model.contextWindow || 1048576,
            contextLimit: 1048576,
            maxOutput: 393216,
            maxOutputLimit: 393216,
            inputModalities: ['text', 'image'],
            inputOptions: ['text', 'image'],
            webSearch: false,
            thinking: { toggle: true, efforts: ['low', 'high'] },
            source: 'catalog',
            locked: ['thinking'],
          },
        ]),
      ),
    }));
    bothAnswer();
    await startWithBothModels();
    await openAdvanced();
    const context = inputOf('上下文长度')!;
    expect(context.readOnly).toBe(false);
    expect(context.placeholder).toBe('1048576');
    expect(host.textContent).toContain('1M');
    act(() => setValue(context, '500000'));
    act(() => setValue(inputOf('上下文长度')!, '9999999'));
    expect(inputOf('上下文长度')!.value).toBe('1048576');
    act(() => setValue(inputOf('上下文长度')!, '200000'));
    expect(inputOf('别名')!.readOnly).toBe(false);
    // Only the thinking comes from the catalog and cannot be changed.
    expect(host.textContent).toContain('可以关闭思考（来自模型目录）');
    // Only web search is a switch; what the catalog says about thinking is not.
    expect(host.querySelectorAll('[role="dialog"] button[role="switch"]').length).toBe(1);
    // The catalog's name is the alias the user starts from.
    expect(inputOf('别名')!.value).toBe('DeepSeek V4');
    // The browser's list of saved entries must not open over the form.
    expect([...host.querySelectorAll<HTMLInputElement>('[role="dialog"] input:not([type="password"])')].every((input) => input.autocomplete === 'off')).toBe(true);
    await act(async () => {
      byText('测试并添加')!.click();
    });
    const models = state.save.mock.calls[0][0].body.models as Array<Record<string, unknown>>;
    expect(models.find((model) => model.id === 'deepseek-flash')).toMatchObject({ contextWindow: 200000 });
  });

  it('removes a provider without touching the profile settings', async () => {
    state.providers = [official, provider];
    state.remove.mockReset().mockResolvedValue({ deleted: true });
    render();
    act(() => byText('移除')!.click());
    const confirm = [...host.querySelectorAll<HTMLButtonElement>('[role="alertdialog"] button')].find((button) => button.textContent?.includes('移除'));
    await act(async () => {
      confirm!.click();
    });
    expect(state.remove).toHaveBeenCalledWith('deepseek');
    expect(state.update).not.toHaveBeenCalled();
  });

  it('does not offer the ChatGPT account as a vendor to add', () => {
    state.presets = [preset, { ...preset, id: 'openai', name: 'OpenAI', short: 'OA', official: true, authMode: 'chatgpt', endpoints: [], models: [] }];
    state.providers = [official];
    render();
    act(() => byText('添加供应商')!.click());
    const dialog = host.querySelector('[role="dialog"]')!;
    const vendors = [...dialog.querySelectorAll('button')].map((button) => button.textContent ?? '');
    expect(vendors.some((label) => label.includes('DeepSeek'))).toBe(true);
    expect(vendors.some((label) => label.includes('OpenAI'))).toBe(false);
  });
});
