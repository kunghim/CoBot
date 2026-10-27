import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SearchBox from './SearchBox';

const state = vi.hoisted(() => ({
  navigate: vi.fn(),
  pathname: '/',
  setBotId: vi.fn(),
  writeLocal: vi.fn(),
  results: {} as Record<string, { data?: { chats: Array<{ chatId: string; title: string; snippet: string }>; members: Array<{ teamId: string; teamName: string; botId: string; botName: string; snippet: string }> }; isFetching?: boolean }>,
}));

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
vi.mock('react-router-dom', () => ({
  useNavigate: () => state.navigate,
  useLocation: () => ({ pathname: state.pathname }),
}));
vi.mock('../../app/SelectedBotContext', () => ({ useSelectedBot: () => ({ setBotId: state.setBotId }) }));
vi.mock('../../app/mode', () => ({ writeLocal: state.writeLocal }));
vi.mock('../../api/queries', () => ({
  useConversationSearch: (q: string) => state.results[q] ?? { data: undefined, isFetching: false },
}));

const type = (value: string) => {
  const input = document.querySelector('input[aria-label="searchBox.placeholder"]')!;
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!;
  act(() => {
    setter.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });
};
const key = (input: HTMLElement, k: string, isComposing = false) => act(() => input.dispatchEvent(new KeyboardEvent('keydown', { key: k, isComposing, bubbles: true })));

describe('conversation search box', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    vi.useFakeTimers();
    Element.prototype.scrollIntoView = vi.fn(); // jsdom does not implement it
    state.navigate.mockReset();
    state.setBotId.mockReset();
    state.writeLocal.mockReset();
    state.pathname = '/';
    state.results = {};
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(async () => {
    act(() => root.unmount());
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    host.remove();
    vi.useRealTimers();
  });

  it('jumps to the chat a settled query matched', async () => {
    state.results['部署'] = { data: { chats: [{ chatId: 'chat-1', title: '部署方案', snippet: '…部署结论…' }], members: [] } };
    act(() => root.render(h(SearchBox)));
    type('部署');
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });
    const option = document.querySelector('[role="option"]');
    expect(option?.textContent).toContain('部署方案');
    const input = document.querySelector('input')!;
    key(input, 'Enter');
    expect(state.navigate).toHaveBeenCalledExactlyOnceWith('/chat/chat-1');
    expect(state.setBotId).not.toHaveBeenCalled();
    expect(state.writeLocal).not.toHaveBeenCalled();
    // Arriving at the chat must not swallow the list the user reopens by
    // typing the next query right away.
    state.pathname = '/chat/chat-1';
    act(() => root.render(h(SearchBox)));
    type('部署2');
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });
    expect(document.querySelector('[role="listbox"]')).not.toBeNull();
  });

  it('ignores Enter while an IME composition is confirming', async () => {
    state.results['预'] = { data: { chats: [{ chatId: 'chat-1', title: '预算', snippet: '' }], members: [] } };
    act(() => root.render(h(SearchBox)));
    type('预');
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });
    const input = document.querySelector('input')!;
    key(input, 'Enter', true);
    expect(state.navigate).not.toHaveBeenCalled();
    key(input, 'Enter');
    expect(state.navigate).toHaveBeenCalledExactlyOnceWith('/chat/chat-1');
  });

  it('selects the matched member, records work mode and opens the team view', async () => {
    state.results['预算'] = { data: { chats: [], members: [{ teamId: 'team-1', teamName: '产品迭代', botId: 'bot-1', botName: '工程师', snippet: '预算又要超了' }] } };
    act(() => root.render(h(SearchBox)));
    type('预算');
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });
    const input = document.querySelector('input')!;
    key(input, 'ArrowDown');
    key(input, 'Enter');
    expect(state.writeLocal).toHaveBeenCalledExactlyOnceWith('cobot.mode', 'work');
    expect(state.setBotId).toHaveBeenCalledExactlyOnceWith('bot-1');
    expect(state.navigate).toHaveBeenCalledExactlyOnceWith('/');
  });

  it('closes without a request while the field is empty', async () => {
    act(() => root.render(h(SearchBox)));
    type('   ');
    await act(async () => { await vi.advanceTimersByTimeAsync(250); });
    expect(document.querySelector('[role="listbox"]')).toBeNull();
    expect(state.results['   ']).toBeUndefined();
  });
});
