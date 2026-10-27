import { expect, it, vi } from 'vitest';
import { pageMatches, searchPDF } from './pdfSearch';
it('maps matches across text spans and line boundaries without including marked-content records', () => {
  expect(pageMatches([{ str: 'hel' }, { type: 'beginMarkedContent' }, { str: 'lo', hasEOL: true }, { str: 'world' }], 'hello world', 3, 200)).toEqual([{ page: 3, parts: [{ item: 0, start: 0, end: 3 }, { item: 1, start: 0, end: 2 }, { item: 2, start: 0, end: 5 }] }]);
  expect(pageMatches([{ str: 'Hello hello' }], 'hello', 1, 200)).toEqual([{ page: 1, parts: [{ item: 0, start: 6, end: 11 }] }]);
});
it('stops at the result limit rather than scanning subsequent pages', async () => {
  const getPage = vi.fn(async () => ({ getTextContent: async () => ({ items: [{ str: 'x '.repeat(205) }] }) }));
  const result = await searchPDF({ numPages: 500, getPage }, 'x', new AbortController().signal);
  expect(result.matches).toHaveLength(200); expect(result.limited).toBe(true); expect(getPage).toHaveBeenCalledTimes(1);
});
it('searches all pages when needed and returns no-match results', async () => {
  const getPage = vi.fn(async (page: number) => ({ getTextContent: async () => ({ items: [{ str: page === 2 ? 'target' : 'other' }] }) }));
  const result = await searchPDF({ numPages: 3, getPage }, 'target', new AbortController().signal);
  expect(result.matches[0].page).toBe(2); expect(result.limited).toBe(false); expect(getPage).toHaveBeenCalledTimes(3);
  expect((await searchPDF({ numPages: 3, getPage }, 'missing', new AbortController().signal)).matches).toEqual([]);
});
it('cancels before fetching more pages and bounds page text', async () => {
  const controller = new AbortController();
  const getPage = vi.fn(async () => ({ getTextContent: async () => { controller.abort(); return { items: [] }; } }));
  await expect(searchPDF({ numPages: 100, getPage }, 'a', controller.signal)).rejects.toMatchObject({ name: 'AbortError' });
  expect(getPage).toHaveBeenCalledTimes(1);
  expect(() => pageMatches([{ str: 'x'.repeat(2 * 1024 * 1024 + 1) }], 'a', 1, 200)).toThrow('PDF search limit');
});
