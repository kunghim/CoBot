import { act, createElement } from 'react';
import { createRoot } from 'react-dom/client';
import { describe, expect, it, vi } from 'vitest';
import { Tree } from './Tree';

describe('Tree with asynchronously loaded directories', () => {
  it('gives a newly loaded empty folder keyboard focus and controlled expansion', () => {
    const container = document.createElement('div'); document.body.append(container);
    const root = createRoot(container); const expanded = vi.fn(); const selected = vi.fn();
    try {
      act(() => root.render(createElement(Tree, { label: 'Files', nodes: [], onSelect: selected })));
      act(() => root.render(createElement(Tree, { label: 'Files', nodes: [{ id: 'empty', label: 'empty', directory: true }], expanded: [], onExpandedChange: expanded, onSelect: selected })));
      const item = container.querySelector<HTMLElement>('[role=treeitem]')!;
      expect(item.tabIndex).toBe(0);
      expect(item.getAttribute('aria-expanded')).toBe('false');
      act(() => item.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })));
      expect(expanded).toHaveBeenCalledWith(['empty']);
      expect(selected).not.toHaveBeenCalled();
      act(() => root.render(createElement(Tree, { label: 'Files', nodes: [{ id: 'blocked', label: 'outside', disabled: true }], onSelect: selected })));
      const blocked = container.querySelector<HTMLElement>('[role=treeitem]')!;
      act(() => blocked.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
      expect(selected).not.toHaveBeenCalled();
    } finally { act(() => root.unmount()); container.remove(); }
  });
});
