// Signing out ends the session on the core: the page session is long-lived,
// and a shared machine needs a way to close it.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import { SecuritySection } from './SettingsSections';

const state = vi.hoisted(() => ({ signOut: vi.fn() }));

vi.mock('../../api/queries', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../api/queries')>();
  return {
    ...actual,
    useAuthorizations: () => ({ data: { items: [] }, isPending: false, isSuccess: true }),
    useRevokeAuthorization: () => ({ mutate: vi.fn() }),
    useSessionLogout: () => ({ mutate: state.signOut, isPending: false }),
  };
});

describe('signing out of CoBot', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    state.signOut.mockReset().mockResolvedValue({ loggedOut: true });
  });

  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const byText = (text: string) =>
    [...host.querySelectorAll('button')].find((button) => button.textContent?.includes(text));

  it('offers a way to end the session and asks the core to end it', () => {
    act(() => root.render(h(SecuritySection, {})));
    const button = byText('退出 CoBot');
    expect(button).toBeTruthy();
    act(() => button!.click());
    expect(state.signOut).toHaveBeenCalledTimes(1);
  });
});
