// The ChatGPT account signs in on its own provider card: the engine runs
// whichever provider a member chose, and this is the one provider whose
// credential is a browser sign-in rather than a key.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n';
import ProviderSection from './ProviderSection';

const fixture = vi.hoisted(() => ({
  info: { status: 'ready', loggedIn: false },
  refetch: vi.fn(),
  mutate: vi.fn(),
}));
vi.mock('../../api/queries', () => ({
  useModelCatalog: () => ({ data: undefined }),
  useUpdateModelCatalog: () => ({ mutate: vi.fn(), isPending: false, data: undefined }),
  useApplyEngineCatalog: () => ({ mutate: vi.fn(), isPending: false }),
  useEngine: () => ({ data: fixture.info, refetch: fixture.refetch }),
  useEngineLogin: () => ({ mutate: fixture.mutate, isPending: false }),
  useEngineLoginCancel: () => ({ mutate: vi.fn(), isPending: false }),
  useEngineLogout: () => ({ mutate: vi.fn(), isPending: false }),
  useProviders: () => ({
    data: { providers: [{ id: 'openai', name: 'OpenAI', kind: 'official', authMode: 'chatgpt', endpoints: [], models: [], enabled: true, revision: 1, presetId: 'openai' }] },
    isPending: false,
    isSuccess: true,
    isError: false,
  }),
  useProviderPresets: () => ({ data: { presets: [] } }),
  useDeleteProvider: () => ({ mutateAsync: vi.fn() }),
  useSetProviderEnabled: () => ({ mutate: vi.fn() }),
  useReprobeProvider: () => ({ mutateAsync: vi.fn() }),
  useSaveProvider: () => ({ mutateAsync: vi.fn() }),
  useEngineModels: () => ({ data: { models: [{ id: 'gpt-test', label: 'GPT Test', isDefault: true, contextWindow: 272000, supportedEfforts: ['low', 'medium', 'high'], inputModalities: ['text'] }] }, isPending: false }),
}));
vi.mock('../../components/ui/Toast', () => ({ useToast: () => ({ show: vi.fn(), dismiss: vi.fn() }) }));

const authURL = 'https://auth.openai.com/oauth/authorize?state=test-only';

describe('ChatGPT sign-in on the provider card', () => {
  let host: HTMLDivElement;
  let root: Root;
  const nativeOpen = vi.fn(async (_url: string) => {});
  const render = () => act(() => root.render(h(ProviderSection, { settings: { defaultProvider: '', defaultModel: '' } as never, save: vi.fn(), errors: {} })));
  const clickLogin = async () => {
    const button = Array.from(host.querySelectorAll('button')).find((node) => node.textContent === i18n.t('engine.login'));
    expect(button).toBeDefined();
    await act(async () => button!.click());
  };
  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    fixture.info = { status: 'ready', loggedIn: false };
    fixture.mutate.mockReset().mockImplementation((_args, handlers) => handlers.onSuccess({ authUrl: authURL }));
    nativeOpen.mockReset().mockResolvedValue(undefined);
    Object.defineProperty(window, 'go', { configurable: true, value: { desktop: { Bridge: { OpenLoginURL: nativeOpen } } } });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
    Reflect.deleteProperty(window, 'go');
  });

  it('says the account is signed out and offers to sign in', () => {
    render();
    expect(host.textContent).toContain(i18n.t('providers.accountSignedOut'));
  });

  it('says the account is signed in and offers to sign out', () => {
    fixture.info.loggedIn = true;
    render();
    expect(host.textContent).toContain(i18n.t('providers.accountSignedIn'));
    expect(Array.from(host.querySelectorAll('button')).some((node) => node.textContent === i18n.t('engine.logout'))).toBe(true);
    expect(nativeOpen).not.toHaveBeenCalled();
  });

  it('automatically opens official authorization with the desktop bridge and reuses it on retry', async () => {
    render();
    await clickLogin();
    expect(nativeOpen).toHaveBeenCalledExactlyOnceWith(authURL);
    const link = host.querySelector('a')!;
    const event = new MouseEvent('click', { bubbles: true, cancelable: true });
    await act(async () => { link.dispatchEvent(event); });
    expect(event.defaultPrevented).toBe(true);
    expect(nativeOpen).toHaveBeenCalledTimes(2);
  });

  it('keeps the sign-in link available when the native browser handoff fails', async () => {
    nativeOpen.mockRejectedValue(new Error('fixture failure'));
    render();
    await clickLogin();
    expect(host.querySelector('[role="alert"]')?.textContent).toContain('无法打开默认浏览器');
    expect(host.querySelector('a')?.href).toBe(authURL);
  });

  it('keeps a normal browser link for the Web version without requiring the desktop bridge', async () => {
    Reflect.deleteProperty(window, 'go');
    render();
    await clickLogin();
    expect(nativeOpen).not.toHaveBeenCalled();
    const link = host.querySelector('a')!;
    expect(link.href).toBe(authURL);
    expect(link.target).toBe('_blank');
  });

  it('clears the pending authorization link only when the engine confirms sign-in', async () => {
    render();
    await clickLogin();
    expect(host.querySelector('a')).not.toBeNull();
    fixture.info.loggedIn = true;
    render();
    expect(host.querySelector('a')).toBeNull();
    expect(host.textContent).toContain(i18n.t('providers.accountSignedIn'));
  });
});
