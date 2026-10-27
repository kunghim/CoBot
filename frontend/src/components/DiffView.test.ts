import { createElement } from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import { expect, it } from 'vitest';
import DiffView from './DiffView';

it('keeps hunk context outside a rendered window and bounds its DOM', () => {
  const lines = ['diff --git a/a b/a', '--- a/a', '+++ b/a', '@@ -1 +1 @@', '--- actual old content', '+++ actual new content'];
  const html = renderToStaticMarkup(createElement(DiffView, { diff: lines.join('\n'), startLine: 5, maxLines: 1 }));
  const document = new DOMParser().parseFromString(html, 'text/html');
  expect(document.querySelectorAll('pre > div')).toHaveLength(1);
  expect(document.querySelector('div')?.className).toContain('bg-green-50');
  expect(document.querySelector('div')?.textContent).toBe('+++ actual new content');
  const header = renderToStaticMarkup(createElement(DiffView, { diff: lines.join('\n'), startLine: 2, maxLines: 1 }));
  expect(header).not.toContain('bg-green-50');
});
