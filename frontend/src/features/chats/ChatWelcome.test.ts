import { act, createElement as h } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, expect, it, vi } from 'vitest';
import ChatWelcome, { greetingPeriod } from './ChatWelcome';

vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: (key: string) => key }) }));
afterEach(() => vi.useRealTimers());

it('uses local day periods at their boundaries, including midnight', () => {
  const cases = [[0, 'night'], [4, 'night'], [5, 'morning'], [8, 'morning'], [9, 'forenoon'], [11, 'forenoon'], [12, 'noon'], [13, 'noon'], [14, 'afternoon'], [17, 'afternoon'], [18, 'evening'], [22, 'evening'], [23, 'night']] as const;
  for (const [hour, expected] of cases) expect(greetingPeriod(hour)).toBe(expected);
});

it('refreshes the time greeting while idle and keeps it stable with a draft', () => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 9, 24, 8, 59));
  const host = document.createElement('div');
  const root = createRoot(host);
  try {
    act(() => root.render(h(ChatWelcome)));
    expect(host.textContent).toBe('chat.greeting.morning');
    act(() => vi.advanceTimersByTime(60_000));
    expect(host.textContent).toBe('chat.greeting.forenoon');
    act(() => root.render(h(ChatWelcome, { hasDraft: true })));
    vi.setSystemTime(new Date(2026, 9, 24, 19));
    act(() => { vi.advanceTimersByTime(60_000); window.dispatchEvent(new Event('focus')); });
    expect(host.textContent).toBe('chat.greeting.forenoon');
    act(() => root.render(h(ChatWelcome, { hasDraft: false })));
    expect(host.textContent).toBe('chat.greeting.evening');
  } finally { act(() => root.unmount()); }
});
