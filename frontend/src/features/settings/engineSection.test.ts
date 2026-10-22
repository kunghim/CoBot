// The engine section reports whether the engine is usable, and nothing
// about accounts: the engine runs whichever provider a member chose, and the
// ChatGPT account signs in on its own provider card.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import i18n from '../../i18n';
import { EngineSection } from './SettingsSections';

const state = vi.hoisted(() => ({ loggedIn: false, status: 'ready' }));

vi.mock('../../api/queries', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/queries')>();
  return {
    ...actual,
    useEngine: () => ({
      data: {
        status: state.status,
        version: '0.161.0',
        loggedIn: state.loggedIn,
        capabilities: ['app-server', 'codexHook:trusted'],
      },
      refetch: vi.fn(),
    }),
    useEngineCheck: () => ({ mutate: vi.fn() }),
  };
});

describe('the engine section', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    state.loggedIn = false;
    state.status = 'ready';
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = () => act(() => root.render(h(EngineSection, {})));
  const buttons = () => [...host.querySelectorAll('button')].map((button) => button.textContent);

  it('reads ready when the engine is ready, signed in or not', () => {
    render();
    const status = host.querySelector('[role="status"]')!;
    expect(status.textContent).toContain(i18n.t('settings.engineReady'));
    expect(status.querySelector('.bg-emerald-500')).not.toBeNull();
  });

  it('offers no sign-in and points to the provider list for the account', () => {
    render();
    expect(buttons()).not.toContain(i18n.t('engine.login'));
    expect(buttons()).not.toContain(i18n.t('engine.logout'));
    expect(host.textContent).toContain(i18n.t('settings.engineAccountHint'));
  });

  it('offers a recheck when the engine is not ready', () => {
    state.status = 'error';
    render();
    expect(buttons()).toContain(i18n.t('engine.recheck'));
  });
});
