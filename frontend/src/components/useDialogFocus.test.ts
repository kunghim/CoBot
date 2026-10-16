import { act, createElement as h, type ReactNode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useDialogFocus } from './useDialogFocus';

function Dialog({ open = true, pending = false, close, children }: { open?: boolean; pending?: boolean; close: () => void; children?: ReactNode }) {
  const ref = useDialogFocus(open, close, pending);
  return open ? h('div', { ref, role: 'dialog', tabIndex: -1 }, children) : null;
}

describe('useDialogFocus mounted behavior', () => {
  let host: HTMLDivElement;
  let trigger: HTMLButtonElement;
  let root: Root;
  const close = vi.fn();
  const render = (props: Partial<Parameters<typeof Dialog>[0]> = {}) => act(() => root.render(h(Dialog, { close, ...props })));
  const key = (value: string, shiftKey = false) => {
    const event = new KeyboardEvent('keydown', { key: value, shiftKey, bubbles: true, cancelable: true });
    act(() => { (document.activeElement ?? document).dispatchEvent(event); });
    return event;
  };
  const buttons = () => [h('button', { key: 'first', id: 'first' }, 'first'), h('button', { key: 'last', id: 'last' }, 'last')];
  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    close.mockReset();
    trigger = document.createElement('button');
    host = document.createElement('div');
    document.body.append(trigger, host);
    trigger.focus();
    root = createRoot(host);
  });
  afterEach(() => { act(() => root.unmount()); host.remove(); trigger.remove(); });

  it('captures the trigger before initial input focus and restores it on close', () => {
    render({ children: [h('button', { key: 'x' }, 'close'), h('input', { key: 'input' })] });
    expect(document.activeElement).toBe(host.querySelector('input'));
    render({ open: false });
    expect(document.activeElement).toBe(trigger);
  });

  it('loops in both directions and recovers focus from outside', () => {
    render({ children: buttons() });
    const [first, last] = host.querySelectorAll('button');
    expect(document.activeElement).toBe(first);
    expect(key('Tab', true).defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(last);
    key('Tab'); expect(document.activeElement).toBe(first);
    key('Tab'); expect(document.activeElement).toBe(last);
    trigger.focus(); key('Tab'); expect(document.activeElement).toBe(first);
  });

  it('filters dynamic disabled, hidden and negative tabindex controls on each key', () => {
    render({ children: [ ...buttons(), h('button', { key: 'disabled', disabled: true }, 'disabled'), h('button', { key: 'negative', tabIndex: -1 }, 'negative'), h('div', { key: 'hidden', hidden: true }, h('button', {}, 'hidden')), h('div', { key: 'css', style: { display: 'none' } }, h('button', {}, 'css')), h('fieldset', { key: 'fieldset', disabled: true }, h('button', {}, 'fieldset')) ] });
    const first = host.querySelector<HTMLButtonElement>('#first')!;
    const last = host.querySelector<HTMLButtonElement>('#last')!;
    last.disabled = true;
    key('Tab'); expect(document.activeElement).toBe(first);
    last.disabled = false;
    key('Tab'); expect(document.activeElement).toBe(last);
    key('Tab'); expect(document.activeElement).toBe(first);
  });

  it('focuses the container when there are no valid controls', () => {
    render({ children: h('button', { disabled: true }, 'disabled') });
    expect(document.activeElement).toBe(host.firstElementChild);
    key('Tab'); expect(document.activeElement).toBe(host.firstElementChild);
    key('Tab', true); expect(document.activeElement).toBe(host.firstElementChild);
  });

  it('does not reset focus on callback, form or role changes and uses current pending state', () => {
    render({ children: buttons() });
    key('Tab');
    const last = document.activeElement;
    const updated = vi.fn();
    render({ close: updated, pending: true, children: [...buttons(), h('input', { key: 'role', value: 'role', readOnly: true })] });
    expect(document.activeElement).toBe(last);
    expect(key('Escape').defaultPrevented).toBe(true);
    expect(updated).toHaveBeenCalledTimes(0);
    render({ close: updated, children: buttons() });
    expect(document.activeElement).toBe(last);
    key('Escape'); expect(updated).toHaveBeenCalledTimes(1);
    expect(close).toHaveBeenCalledTimes(0);
  });

  it('handles Escape only once for the current dialog', () => {
    const parentClose = vi.fn();
    act(() => root.render(h(Dialog, { close: parentClose }, h(Dialog, { close }, h('button', {}, 'inner')))));
    key('Escape');
    expect(close).toHaveBeenCalledTimes(1);
    expect(parentClose).not.toHaveBeenCalled();
  });

  it('restores on unmount but does not focus a removed trigger', () => {
    render({ children: buttons() });
    act(() => root.unmount());
    expect(document.activeElement).toBe(trigger);
    root = createRoot(host);
    render({ children: buttons() });
    trigger.remove();
    const focus = vi.spyOn(trigger, 'focus');
    render({ open: false });
    expect(focus).not.toHaveBeenCalled();
  });
});
