// A relay-posted document (the requirement to confirm, the acceptance
// report) renders as its own card: title line from the document's heading,
// the body as markdown, folding to the title.
import { act, createElement as h } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n';
import DocumentCard from './DocumentCard';

vi.mock('./Markdown', () => ({ default: ({ text }: { text: string }) => h('div', null, text) }));

const doc = '# 每日待办任务静态页面 · 用户确认\n\n### 链路\n\n1. 用户确认（用户）\n\n2. 实现（全栈工程师）';

describe('DocumentCard', () => {
  let root: Root;
  let host: HTMLDivElement;

  beforeEach(() => {
    Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  const render = () => act(() => root.render(h(DocumentCard, { text: doc })));

  it('opens on the document with its heading as the title', () => {
    render();
    expect(host.textContent).toContain('每日待办任务静态页面 · 用户确认');
    expect(host.textContent).toContain('1. 用户确认（用户）');
    expect(host.textContent).toContain('文档');
  });

  it('folds to its title and opens back up', () => {
    render();
    const toggle = host.querySelector('button[aria-expanded]') as HTMLButtonElement;
    act(() => toggle.click());
    expect(host.textContent).not.toContain('1. 用户确认（用户）');
    act(() => toggle.click());
    expect(host.textContent).toContain('1. 用户确认（用户）');
  });
});
