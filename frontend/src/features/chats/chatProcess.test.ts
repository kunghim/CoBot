import { describe, expect, it } from 'vitest';
import type { ChatRun } from '../../api/types';
import { chatRunHasContent } from './ChatProcess';

const run = (overrides: Partial<ChatRun>): ChatRun => ({
  id: 'r1', chatId: 'c1', messageId: 'm1', status: 'completed', threadId: 't', turnId: 'u', epoch: 'e', errorKey: '',
  startedAt: '2026-10-25T00:00:00Z', endedAt: '2026-10-25T00:00:05Z', steps: [], ...overrides,
});
const message = (id: string, text: string) => ({ id, seq: 1, kind: 'message', status: 'completed', detail: { text } });
const command = { id: 'c', seq: 2, kind: 'command', status: 'completed', detail: { command: 'ls' } };

describe('a chat turn above its reply', () => {
  it('has nothing to show for a plain answer', () => {
    expect(chatRunHasContent(run({}), '好')).toBe(false);
    // The model said its answer as a step too; the reply already holds it.
    expect(chatRunHasContent(run({ steps: [message('a', '好')] }), '好')).toBe(false);
  });

  it('shows what was said on the way and the work between', () => {
    expect(chatRunHasContent(run({ steps: [message('a', '我先看看'), command, message('b', '看完了')] }), '看完了')).toBe(true);
  });

  it('shows a turn that is running, one that failed, and an error', () => {
    expect(chatRunHasContent(run({ status: 'running', endedAt: '' }), '')).toBe(true);
    expect(chatRunHasContent(run({ status: 'failed' }), '')).toBe(true);
    expect(chatRunHasContent(run({ errorKey: 'error.chat.failed' }), '')).toBe(true);
  });
});
