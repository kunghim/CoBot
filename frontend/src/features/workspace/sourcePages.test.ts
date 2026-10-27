import { describe, expect, it } from 'vitest';
import { sourcePage, SOURCE_PAGE_LINES } from './sourcePages';

describe('source pages', () => {
  it('bounds the rendered lines and returns the exact next byte offset for Unicode and CRLF', () => {
    const text = Array.from({ length: 700 }, (_, index) => `第 ${index + 1} 行`).join('\r\n');
    const bytes = new TextEncoder().encode(text);
    const first = sourcePage(bytes, true);
    expect(first.newlines).toBe(SOURCE_PAGE_LINES);
    expect(first.text).toContain('第 500 行');
    expect(first.text).not.toContain('第 501 行');
    const second = sourcePage(bytes.slice(first.consumed), true);
    expect(second.text.startsWith('第 501 行')).toBe(true);
    expect(first.text + second.text).toBe(text);
  });
  it('does not consume half a UTF-8 rune or a CRLF pair at a chunk boundary', () => {
    const unicode = new TextEncoder().encode('prefix汉');
    const first = sourcePage(unicode.slice(0, unicode.length - 1), false);
    expect(first.text).toBe('prefix');
    expect(first.consumed).toBe(6);
    const second = sourcePage(unicode.slice(first.consumed), true);
    expect(first.text + second.text).toBe('prefix汉');
    const ending = sourcePage(new TextEncoder().encode('one\r'), false);
    expect(ending.text).toBe('one');
    expect(sourcePage(new TextEncoder().encode('\r\ntwo'), true).newlines).toBe(1);
  });
  it('rejects invalid encoding instead of shifting later byte positions', () => {
    expect(() => sourcePage(new Uint8Array([0xff]), true)).toThrow();
    expect(sourcePage(new Uint8Array(), true)).toEqual({ text: '', consumed: 0, newlines: 0 });
  });
});
