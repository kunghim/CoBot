// The engine section reports whether the engine is usable. With a
// third-party provider the ChatGPT account is not part of the setup, so
// asking to sign in would be noise.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import { EngineSection } from './SettingsSections';

const state = vi.hoisted(() => ({
  providers: [] as unknown[],
  defaultProvider: '',
  loggedIn: false,
}));

vi.mock('../../api/queries', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/queries')>();
  return {
    ...actual,
    useEngine: () => ({
      data: {
        status: 'ready',
        version: '0.161.0',
        loggedIn: state.loggedIn,
        capabilities: ['app-server', 'codexHook:trusted'],
      },
      refetch: vi.fn(),
    }),
    useEngineCheck: () => ({ mutate: vi.fn() }),
    useEngineLogin: () => ({ mutate: vi.fn(), isPending: false }),
    useEngineLoginCancel: () => ({ mutate: vi.fn(), isPending: false }),
    useEngineLogout: () => ({ mutate: vi.fn(), isPending: false }),
    useProviders: () => ({ data: { providers: state.providers }, isPending: false }),
    useSettings: () => ({ data: { defaultProvider: state.defaultProvider }, isPending: false }),
  };
});

const gateway = {
  id: 'cpa',
  name: '公司的中转站',
  kind: 'custom',
  authMode: 'api_key',
  endpoints: [{ dialect: 'openai-responses', baseUrl: 'https://cpa.example.invalid/v1' }],
  models: [{ id: 'deepseek-v4-pro' }],
  revision: 1,
};

describe('the engine section under a third-party provider', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    state.providers = [];
    state.defaultProvider = '';
    state.loggedIn = false;
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = () => act(() => root.render(h(EngineSection, {})));

  it('asks for a ChatGPT sign-in when the engine runs on its own account', () => {
    render();
    expect(host.textContent).toContain('请先登录后再使用');
    expect([...host.querySelectorAll('button')].some((b) => b.textContent?.includes('登录'))).toBe(true);
  });

  it('does not ask for a sign-in when a gateway is the provider in use', () => {
    state.providers = [gateway];
    state.defaultProvider = 'cpa';
    render();
    expect(host.textContent).toContain('不需要 ChatGPT 登录');
    expect(host.textContent).not.toContain('请先登录后再使用');
    expect([...host.querySelectorAll('button')].some((b) => b.textContent?.includes('登录'))).toBe(false);
  });

  it('keeps asking for a sign-in when the official provider is the one in use', () => {
    state.providers = [{ ...gateway, id: 'openai', authMode: 'chatgpt' }];
    state.defaultProvider = 'openai';
    render();
    expect(host.textContent).toContain('请先登录后再使用');
  });
});
