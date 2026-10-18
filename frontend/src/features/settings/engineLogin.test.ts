import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n';
import { EngineSection } from './SettingsSections';

const fixture = vi.hoisted(() => ({
  info: { status: 'ready', loggedIn: false, version: '0.161.0', capabilities: ['codexHook:trusted'] },
  refetch: vi.fn(),
  mutate: vi.fn(),
}));
vi.mock('../../api/queries', () => ({
  useEngine: () => ({ data: fixture.info, refetch: fixture.refetch }),
  useEngineLogin: () => ({ mutate: fixture.mutate, isPending: false }),
  useEngineLoginCancel: () => ({ mutate: vi.fn(), isPending: false }),
  useEngineLogout: () => ({ mutate: vi.fn(), isPending: false }),
  useEngineCheck: () => ({ mutate: vi.fn(), isPending: false }),
}));

const authURL = 'https://auth.openai.com/oauth/authorize?state=test-only';

describe('engine sign-in state and browser handoff', () => {
  let host: HTMLDivElement;
  let root: Root;
  const nativeOpen = vi.fn(async (_url: string) => {});
  const render = () => act(() => root.render(h(EngineSection)));
  const clickLogin = async () => {
    const button = Array.from(host.querySelectorAll('button')).find((node) => node.textContent === i18n.t('engine.login'));
    expect(button).toBeDefined();
    await act(async () => button!.click());
  };
  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    fixture.info = { status: 'ready', loggedIn: false, version: '0.161.0', capabilities: ['codexHook:trusted'] };
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

  it('makes sign-in required prominent even when the binary and hook are ready', () => {
    render();
    const status = host.querySelector('[role="status"]')!;
    expect(status.textContent).toContain('未登录');
    expect(status.textContent).not.toContain('引擎可用');
    expect(status.querySelector('.bg-amber-500')).not.toBeNull();
  });

  it('shows green readiness only after the engine reports an account', () => {
    fixture.info.loggedIn = true;
    render();
    const status = host.querySelector('[role="status"]')!;
    expect(status.textContent).toContain('已登录');
    expect(status.querySelector('.bg-green-500')).not.toBeNull();
    expect(nativeOpen).not.toHaveBeenCalled();
  });

  it('automatically opens official authorization with the desktop bridge and reuses it on retry', async () => {
    render();
    await clickLogin();
    expect(nativeOpen).toHaveBeenCalledExactlyOnceWith(authURL);
    expect(host.querySelector('[role="status"]')!.textContent).toContain('未登录');
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
    expect(host.querySelector('[role="status"]')!.textContent).toContain('未登录');
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
    fixture.info.loggedIn = true;
    render();
    expect(host.querySelector('a')).toBeNull();
    expect(host.querySelector('[role="status"] .bg-green-500')).not.toBeNull();
  });
});
