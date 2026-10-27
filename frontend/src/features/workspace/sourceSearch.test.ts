import { expect, it } from 'vitest';
import { searchSource } from './sourceSearch';

async function* chunks(...values: string[]) { yield* values; }
it('finds overlapping literals across chunks with Unicode and CRLF positions', async () => {
  expect(await searchSource(chunks('汉\r', '\na', 'ba', 'ba\naba'), 'aba')).toEqual({ matches: [{ line: 2, column: 1 }, { line: 2, column: 3 }, { line: 3, column: 1 }], limited: false });
});
it('bounds results and handles an empty file and literal metacharacters', async () => {
  expect(await searchSource(chunks('aaaa'), 'a', 2)).toEqual({ matches: [{ line: 1, column: 1 }, { line: 1, column: 2 }], limited: true });
  expect((await searchSource(chunks('a.*b'), '.*')).matches).toEqual([{ line: 1, column: 2 }]);
  expect((await searchSource(chunks(), 'x')).matches).toEqual([]);
});
it('keeps positions stable at every split including CRLF and overlapping results', async () => {
  const text = 'one\r\n汉aba\rabaaba\naba';
  const expected = await searchSource(chunks(text), 'aba');
  for (let split = 0; split <= text.length; split++) {
    expect(await searchSource(chunks(text.slice(0, split), text.slice(split)), 'aba')).toEqual(expected);
  }
});
