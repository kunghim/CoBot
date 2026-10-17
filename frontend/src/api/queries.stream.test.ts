import { act, createElement } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { resetLiveStore, useLiveRuns } from './live';
import { useEventStream } from './queries';
import type { Event } from './types';

class Stream {
  static opened: Stream[] = [];
  onerror: (() => void) | null = null;
  listeners = new Map<string, (event: MessageEvent) => void>();
  closed = false;
  constructor(readonly url: string) { Stream.opened.push(this); }
  addEventListener(name: string, listener: (event: MessageEvent) => void) { this.listeners.set(name, listener); }
  close() { this.closed = true; }
  emit(sequence: number) { this.listeners.get('cobot.event')?.({ data: JSON.stringify({ seq: sequence, type: 'test' }) } as MessageEvent); }
}

describe('snapshot and SSE cursor', () => {
  let root: Root;
  let container: HTMLDivElement;
  const onEvent = vi.fn<(event: Event) => void>();
  function Observer({ enabled, sequence }: {enabled: boolean; sequence: number}) {
    useEventStream(enabled, onEvent, sequence);
    return null;
  }
  beforeEach(() => {
    vi.useFakeTimers();
    Stream.opened = [];
    onEvent.mockClear();
    vi.stubGlobal('EventSource', Stream);
    vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true);
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ data: { authorized: true } }), { status: 200 })));
    container = document.createElement('div');
    document.body.append(container);
    root = createRoot(container);
  });
  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  const render = async (enabled: boolean, sequence: number) => { await act(async () => { root.render(createElement(Observer, { enabled, sequence })); }); };

  it('starts after the initial snapshot and does not reopen on later snapshots', async () => {
    await render(false, 0);
    expect(Stream.opened).toHaveLength(0);
    await render(true, 120);
    expect(Stream.opened[0].url).toBe('/api/v1/events?after=120');
    act(() => Stream.opened[0].emit(121));
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ seq: 121 }));
    await render(true, 122);
    expect(Stream.opened).toHaveLength(1);
    expect(Stream.opened[0].closed).toBe(false);
  });

  it('reconnects with the received cursor rather than replaying old history', async () => {
    await render(true, 120);
    act(() => Stream.opened[0].emit(125));
    await act(async () => { Stream.opened[0].onerror?.(); });
    expect(Stream.opened[0].closed).toBe(true);
    await act(async () => { await vi.advanceTimersByTimeAsync(1501); });
    expect(Stream.opened).toHaveLength(2);
    expect(Stream.opened[1].url).toBe('/api/v1/events?after=125');
  });

  it('feeds live frames to the live store without moving the cursor', async () => {
    resetLiveStore();
    let seen: ReturnType<typeof useLiveRuns> = {};
    function Watcher() { seen = useLiveRuns(); return null; }
    await act(async () => { root.render(createElement('div', null, createElement(Observer, { enabled: true, sequence: 7 }), createElement(Watcher))); });
    act(() => Stream.opened[0].listeners.get('cobot.live')?.({ data: JSON.stringify({ type: 'delta', teamId: 't', botId: 'b', runId: 'r1', kind: 'message', text: '你好' }) } as MessageEvent));
    expect(seen.r1?.reply).toBe('你好');
    expect(onEvent).not.toHaveBeenCalled();
    await act(async () => { Stream.opened[0].onerror?.(); });
    await act(async () => { await vi.advanceTimersByTimeAsync(1501); });
    expect(Stream.opened[1].url).toBe('/api/v1/events?after=7');
  });

  it('resets an old session cursor when the stream is disabled', async () => {
    await render(true, 120);
    act(() => Stream.opened[0].emit(125));
    await render(false, 120);
    await render(true, 3);
    expect(Stream.opened[0].closed).toBe(true);
    expect(Stream.opened[1].url).toBe('/api/v1/events?after=3');
  });
});
