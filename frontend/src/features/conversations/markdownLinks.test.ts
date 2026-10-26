// A web link in a member's reply opens the team's own browser — a user tab
// in the workspace, focused — instead of leaving the app; the hover entry
// hands the address to the system browser, and mail stays with the system.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import Markdown from './Markdown';

const state = vi.hoisted(() => ({
  open: vi.fn(),
  openTab: vi.fn(),
  openExternal: vi.fn(),
}));

vi.mock('../../api/queries', () => ({
  useOpenBrowserTab: () => ({ mutate: state.open, isPending: false }),
}));
vi.mock('../workspace/WorkspaceProvider', () => ({
  useWorkspace: () => ({ openTab: state.openTab }),
}));
vi.mock('../../platform/native-bridge', () => ({
  openExternal: state.openExternal,
}));
vi.mock('../../components/ui', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useToast: () => ({ show: vi.fn() }),
}));

describe('web links in replies', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    state.open.mockReset();
    state.openTab.mockReset();
    state.openExternal.mockReset();
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = (text: string, teamId?: string) =>
    act(() => root.render(h(Markdown, { text, teamId })));
  const link = () => host.querySelector('a') as HTMLAnchorElement;

  it('opens a team conversation link in the built-in browser', () => {
    render('[示例](https://example.com/page)', 't1');
    act(() => link().click());
    expect(state.open).toHaveBeenCalledWith('https://example.com/page', expect.anything());
  });

  it('names the domain and offers the system browser beside the link', () => {
    render('[示例](https://example.com/page)', 't1');
    const system = [...host.querySelectorAll('button')].find((button) => button.getAttribute('aria-label') === '在系统浏览器打开');
    expect(system).toBeDefined();
    expect(link().getAttribute('title')).toBe('example.com');
    act(() => system!.click());
    expect(state.openExternal).toHaveBeenCalledWith('https://example.com/page');
  });

  it('leaves mail and chat links to the system', () => {
    render('[写信](mailto:a@b.c)', 't1');
    expect(link().href).toBe('mailto:a@b.c');
    render('[示例](https://example.com/page)');
    act(() => link().click());
    expect(state.open).not.toHaveBeenCalled();
  });
});
