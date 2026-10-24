// What waits for the user's decision (a member's question, a confirm stage, a
// finished requirement) is a card above the composer, like an approval.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import QuestionCardStack from './QuestionCardStack';

const state = vi.hoisted(() => ({
  items: [] as unknown[],
  hasNextPage: false,
  answer: vi.fn(),
  cancel: vi.fn(),
}));

vi.mock('../../api/queries', () => ({
  useQuestions: () => ({ data: { items: state.items }, isError: false, hasNextPage: state.hasNextPage, fetchNextPage: vi.fn() }),
  useAnswerQuestion: () => ({ mutate: state.answer, isPending: false }),
  useCancelQuestion: () => ({ mutate: state.cancel, isPending: false }),
}));
vi.mock('../conversations/Markdown', () => ({ default: ({ text }: { text: string }) => h('div', null, text) }));

const confirm = {
  id: 'q1',
  kind: 'confirm',
  teamId: 't1',
  requesterBotId: 'b1',
  question: '请确认：需求和链路',
  options: [],
  status: 'open',
  revision: 1,
  createdAt: '2026-10-24T00:00:00Z',
};
const ask = { ...confirm, id: 'q2', kind: '', question: '用哪个数据库？', options: ['SQLite', 'Postgres'] };

describe('question cards', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    state.items = [];
    state.hasNextPage = false;
    state.answer.mockReset();
    state.cancel.mockReset();
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = () => act(() => root.render(h(QuestionCardStack, { teamId: 't1', viewBotId: 'b1' })));
  const byText = (text: string) => [...host.querySelectorAll('button')].find((button) => button.textContent?.includes(text));

  it('shows nothing when nothing waits', () => {
    render();
    expect(host.textContent).toBe('');
  });

  it('puts a confirmation in a card that only asks, and accepts it from there', () => {
    state.items = [confirm];
    render();
    expect(host.textContent).toContain('需要你确认');
    expect(host.textContent).toContain('请确认：需求和链路');
    // The document is in the conversation, not in the card.
    expect(byText('查看完整文档')).toBeUndefined();
    // "Ask for changes" needs a word on what to change.
    expect((byText('提修改意见') as HTMLButtonElement).disabled).toBe(true);
    act(() => byText('确认，继续')!.click());
    expect(state.answer).toHaveBeenCalledWith(
      expect.objectContaining({ questionId: 'q1', decision: 'accept', answerText: '', expectedRevision: 1 }),
      expect.anything(),
    );
  });

  it('answers a question with one of its options', () => {
    state.items = [ask];
    render();
    expect(host.textContent).toContain('用哪个数据库？');
    act(() => byText('Postgres')!.click());
    expect(state.answer).toHaveBeenCalledWith(expect.objectContaining({ questionId: 'q2', answerText: 'Postgres' }), expect.anything());
  });

  it('pages through several cards', () => {
    state.items = [confirm, ask];
    render();
    expect(host.textContent).toContain('1 / 2');
    act(() => (host.querySelector('button[aria-label="下一张卡片"]') as HTMLButtonElement).click());
    expect(host.textContent).toContain('用哪个数据库？');
    expect(host.textContent).toContain('2 / 2');
  });
});
