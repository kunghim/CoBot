import { describe, expect, it } from 'vitest';
import { columnName, parseDelimited } from './delimited';
describe('delimited tables', () => {
  it('preserves quoted separators, newlines, escapes, empty values and BOM', () => {
    expect(parseDelimited('\uFEFFname,value,notes\r\n"a,b","line\nnext","say ""hello"""\r\nlast,,\r\n', ',')).toEqual({ columns: 3, rows: [['name', 'value', 'notes'], ['a,b', 'line\nnext', 'say "hello"'], ['last', '', '']] });
  });
  it('supports tabs, uneven rows, empty tables and literal formulas', () => {
    expect(parseDelimited('', ',')).toEqual({ rows: [], columns: 0 });
    expect(parseDelimited('a\tb\n=1+2\t\t<script>\n', '\t')).toEqual({ columns: 3, rows: [['a', 'b'], ['=1+2', '', '<script>']] });
    expect(parseDelimited('""', ',').rows).toEqual([['']]);
    expect(parseDelimited('\n', ',').rows).toEqual([['']]);
  });
  it('rejects malformed quoting and oversized cell dimensions', () => {
    for (const text of ['"unclosed', 'bad"quote', '"closed"bad']) expect(() => parseDelimited(text, ',')).toThrow('Invalid table');
    expect(() => parseDelimited(','.repeat(16384), ',')).toThrow('Table limit');
  });
  it('labels columns through the Excel column range', () => {
    expect([0, 25, 26, 51, 52, 16383].map(columnName)).toEqual(['A', 'Z', 'AA', 'AZ', 'BA', 'XFD']);
  });
});
