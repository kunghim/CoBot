export const MAX_TABLE_BYTES = 10 * 1024 * 1024;
export type TableData = { rows: string[][]; columns: number };

/** Parse quoted delimited text without interpreting formulas or HTML. */
export function parseDelimited(input: string, delimiter: ',' | '\t'): TableData {
  if (new TextEncoder().encode(input).length > MAX_TABLE_BYTES) throw new Error('Table limit');
  const text = input.replace(/^\uFEFF/, '');
  if (!text) return { rows: [], columns: 0 };
  const rows: string[][] = [];
  let row: string[] = [];
  let value = '';
  let quoted = false;
  let closed = false;
  let cells = 0;
  let columns = 0;
  const field = () => {
    if (++cells > 1_000_000 || row.length >= 16384) throw new Error('Table limit');
    row.push(value); value = ''; closed = false;
  };
  const line = () => {
    field(); columns = Math.max(columns, row.length); rows.push(row); row = [];
    if (rows.length > 100_000) throw new Error('Table limit');
  };
  for (let index = 0; index < text.length; index++) {
    const char = text[index];
    if (quoted) {
      if (char === '"') {
        if (text[index + 1] === '"') { value += '"'; index++; }
        else { quoted = false; closed = true; }
      } else value += char;
    } else if (char === delimiter) field();
    else if (char === '\r' || char === '\n') { line(); if (char === '\r' && text[index + 1] === '\n') index++; }
    else if (char === '"' && !value && !closed) quoted = true;
    else if (char === '"' || closed) throw new Error('Invalid table');
    else value += char;
  }
  if (quoted) throw new Error('Invalid table');
  if (row.length || value || closed || !/[\r\n]$/.test(text)) line();
  return { rows, columns };
}

export function columnName(index: number): string {
  let name = '';
  for (let value = index + 1; value > 0; value = Math.floor((value - 1) / 26)) name = String.fromCharCode(65 + (value - 1) % 26) + name;
  return name;
}
