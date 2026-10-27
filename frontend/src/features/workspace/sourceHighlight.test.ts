import { expect, it } from 'vitest';
import hljs from 'highlight.js';
import { highlightLines } from './sourceHighlight';

it('keeps multiline token state and source text while exposing only highlight classes', () => {
  const source = '/* one\n two */\nconst text = "<script>attack()</script>";';
  const lines = highlightLines(hljs.highlight(source, { language: 'typescript' }).value);
  expect(lines.map((tokens) => tokens.map((token) => token.text).join('')).join('\n')).toBe(source);
  expect(lines[0][0].classes).toBe('hljs-comment');
  expect(lines[1][0].classes).toBe('hljs-comment');
  expect(lines[2].some((token) => token.classes.includes('hljs-keyword'))).toBe(true);
  expect(highlightLines('<img src="x" onerror="attack()"><span class="hljs-keyword arbitrary">safe</span>')).toEqual([[{ text: 'safe', classes: 'hljs-keyword' }]]);
});

it('bounds generated markup and token counts', () => {
  expect(highlightLines('x'.repeat(2 * 1024 * 1024 + 1))).toEqual([]);
  expect(highlightLines('<span>x</span>'.repeat(10_001))).toEqual([]);
});
