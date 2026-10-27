// The workspace picture is fitted into its box; the box's size must arrive
// even when the measured element mounts late — a first render that took an
// early return left the old observer unattached forever, and "fit" silently
// became 100%.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useElementSize } from './useElementSize';

// A controllable stand-in for ResizeObserver: the tests decide when a
// resize is reported and at what size.
class FakeResizeObserver {
  static instances: FakeResizeObserver[] = [];
  callback: ResizeObserverCallback;
  constructor(callback: ResizeObserverCallback) {
    this.callback = callback;
    FakeResizeObserver.instances.push(this);
  }
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
  report(width: number, height: number): void {
    this.callback([{ contentRect: { width, height, x: 0, y: 0, top: 0, right: 0, bottom: 0, left: 0 } } as ResizeObserverEntry], this as unknown as ResizeObserver);
  }
}

function Probe({ show }: { show: boolean }) {
  const [ref, size] = useElementSize<HTMLDivElement>();
  return h('div', null, show ? h('div', { ref, 'data-testid': 'box' }) : null, h('span', null, `${size.width}x${size.height}`));
}

describe('useElementSize', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    FakeResizeObserver.instances = [];
    vi.stubGlobal('ResizeObserver', FakeResizeObserver);
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
    vi.unstubAllGlobals();
  });

  it('measures an element that is there from the start', () => {
    act(() => root.render(h(Probe, { show: true })));
    act(() => FakeResizeObserver.instances[0].report(320, 200));
    expect(host.textContent).toContain('320x200');
  });

  it('measures an element that shows up after an early return', () => {
    act(() => root.render(h(Probe, { show: false })));
    expect(host.textContent).toContain('0x0');
    act(() => root.render(h(Probe, { show: true })));
    const observer = FakeResizeObserver.instances[FakeResizeObserver.instances.length - 1];
    act(() => observer.report(320, 200));
    expect(host.textContent).toContain('320x200');
  });
});
