import { afterEach, describe, expect, it } from 'vitest';
import { lastChatPath, readLocal, savedMode, writeLocal } from './mode';

afterEach(() => localStorage.clear());
describe('mode persistence', () => {
  it('keeps the previous mode and conversation without accepting an arbitrary route', () => {
    expect(savedMode()).toBeNull();
    writeLocal('cobot.mode', 'work'); expect(savedMode()).toBe('work');
    writeLocal('cobot.chat.selected', 'chat-123'); expect(lastChatPath()).toBe('/chat/chat-123');
    writeLocal('cobot.chat.selected', '../settings'); expect(lastChatPath()).toBe('/chat/new');
    writeLocal('cobot.mode', 'invalid'); expect(savedMode()).toBeNull();
  });
  it('does not mix chat drafts', () => {
    writeLocal('cobot.chat.draft.one', 'first'); writeLocal('cobot.chat.draft.two', 'second');
    expect(readLocal('cobot.chat.draft.one')).toBe('first'); expect(readLocal('cobot.chat.draft.two')).toBe('second');
  });
});
