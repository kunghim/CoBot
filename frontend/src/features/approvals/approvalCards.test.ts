// Each type of approval card shows what it asks in its own way; moving past the
// last card loaded asks for the next page.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import ApprovalCardStack from './ApprovalCardStack';

const state = vi.hoisted(() => ({
  items: [] as unknown[],
  total: undefined as number | undefined,
  hasNextPage: false,
  fetchNextPage: vi.fn(),
}));

vi.mock('../../api/queries', () => ({
  useApprovals: () => ({ data: { items: state.items, total: state.total ?? state.items.length }, isError: false, hasNextPage: state.hasNextPage, isFetchNextPageError: false, fetchNextPage: state.fetchNextPage }),
  useDecideApproval: () => ({ mutate: vi.fn(), isPending: false }),
}));

const card = (overrides: Record<string, unknown>) => ({
  id: 'a', teamId: 't', requesterBotId: 'b', revision: 1, requestState: 'pending', createdAt: '2026-10-17T10:00:00Z', options: [{ id: 'accept' }], summary: 'plain words', ...overrides,
});

describe('approval cards', () => {
  let host: HTMLElement;
  let root: Root;
  const render = () => act(() => root.render(h(ApprovalCardStack, { teamId: 't', viewBotId: 'b', botNameById: new Map() })));

  beforeEach(() => {
    (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
    state.hasNextPage = false;
    state.total = undefined;
    state.fetchNextPage.mockReset();
    host = document.createElement('div');
    document.body.appendChild(host);
    root = createRoot(host);
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  it('shows a member\'s command in the native body', () => {
    state.items = [card({ type: 'native_tool', method: 'item/commandExecution/requestApproval', summary: JSON.stringify({ command: 'git push' }) })];
    render();
    expect(host.textContent).toContain('git push');
    expect(host.textContent).not.toContain('{"command"');
  });

  it('shows the form of an elicitation, and its words when there is no form', () => {
    state.items = [card({ type: 'native_tool', method: 'mcpServer/elicitation/request', summary: JSON.stringify({ mode: 'form', message: 'fill this in', requestedSchema: { type: 'object', properties: { a: { type: 'string', title: 'Field A' } } } }) })];
    render();
    expect(host.querySelector('input')).not.toBeNull();
    state.items = [card({ id: 'a2', type: 'native_tool', method: 'mcpServer/elicitation/request', summary: 'no form here' })];
    render();
    expect(host.querySelector('input')).toBeNull();
    expect(host.textContent).toContain('no form here');
  });

  it.each(['recovery', 'chain', 'budget', 'invented'])('shows the summary of a %s card', (type) => {
    state.items = [card({ type })];
    render();
    expect(host.querySelector('pre')?.textContent).toBe('plain words');
  });

  it('asks for the next page only past the last card, and stays put if that fails', async () => {
    state.items = [card({ id: 'a1', type: 'budget', summary: 'first' }), card({ id: 'a2', type: 'budget', summary: 'second' })];
    state.hasNextPage = true;
    state.total = 3;
    state.fetchNextPage.mockResolvedValue({ isError: true });
    render();
    const next = () => host.querySelector('button[aria-label="下一张审批卡"]') as HTMLButtonElement;
    act(() => next().click());
    expect(host.textContent).toContain('second');
    expect(state.fetchNextPage).not.toHaveBeenCalled();
    await act(async () => next().click());
    expect(state.fetchNextPage).toHaveBeenCalledTimes(1);
    expect(host.textContent).toContain('second');
  });
});
