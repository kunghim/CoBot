// The workspace answers what shown content asks: a file link and a web link in
// a member's reply open as tabs of the workspace they are shown in.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import { api } from '../../api/client';
import Markdown from '../../components/content/Markdown';
import { WorkspaceProvider, useWorkspace } from './WorkspaceProvider';

vi.mock('../../api/queries', () => ({
  useOpenBrowserTab: () => ({ mutate: (_: string, options: { onSuccess: (result: unknown) => void }) => options.onSuccess({ tab: { id: 'tab1', title: '' } }), isPending: false }),
}));
vi.mock('../../components/ui', async (importOriginal) => ({
  ...(await importOriginal<object>()),
  useToast: () => ({ show: vi.fn() }),
}));


describe('content inside the workspace', () => {
  let root: Root;
  let host: HTMLDivElement;
  let tabs: Array<{ kind: string; title: string; ref: string }> = [];
  function Probe() {
    tabs = useWorkspace().state.tabs;
    return null;
  }

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    localStorage.clear();
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
    vi.restoreAllMocks();
  });

  const click = async (text: string) => {
    await act(async () => root.render(h(WorkspaceProvider, { teamId: 't1', children: [h(Markdown, { key: 'text', text, teamId: 't1' }), h(Probe, { key: 'probe' })] })));
    await act(async () => (host.querySelector('a') as HTMLAnchorElement).click());
  };

  it('opens a file link as a file tab', async () => {
    vi.spyOn(api, 'workspaceFile').mockResolvedValue({ path: 'src/a.ts', name: 'a.ts' } as never);
    await click('[文件](src/a.ts)');
    expect(tabs).toMatchObject([{ kind: 'file', title: 'a.ts', ref: 'src/a.ts' }]);
  });

  it('opens a web link as a browser tab named after its host until the page has a title', async () => {
    await click('[示例](https://example.com/page)');
    expect(tabs).toMatchObject([{ id: 'browser:tab1', kind: 'browser', title: 'example.com', ref: 'tab1' }]);
  });
});
