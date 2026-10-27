// @vitest-environment node
import { describe, expect, it } from 'vitest';
import { utils, write } from 'xlsx';
import { parseWorkbook } from './workbook';
import { validateWorkbookArchive } from './workbookArchive';
function fixture(compression = false, bookType: 'xlsx' | 'xlsm' = 'xlsx'): ArrayBuffer {
  const book = utils.book_new();
  utils.book_append_sheet(book, utils.aoa_to_sheet([['name', 'value'], ['hello', 42], ['<script>', '=literal']]), 'First');
  utils.book_append_sheet(book, { A1: { t: 'n', v: 3, f: '1+2' }, '!ref': 'A1' }, 'Second');
  return write(book, { type: 'array', bookType, compression });
}
describe('workbook parsing', () => {
  it.each([false, true])('reads sheets, literal strings and cached formula results (compressed=%s)', async (compression) => {
    const data = await parseWorkbook(fixture(compression));
    expect(data.sheets.map((sheet) => sheet.name)).toEqual(['First', 'Second']);
    expect(data.sheets[0].data.rows[2]).toEqual(['<script>', '=literal']);
    expect(data.sheets[1].data.rows).toEqual([['3']]);
  });
  it('reads XLSM cells without requesting VBA', async () => {
    expect((await parseWorkbook(fixture(true, 'xlsm'))).sheets).toHaveLength(2);
  });

  it.each([false, true])('preserves saved date, percentage, error and empty-sheet values (1904=%s)', async (date1904) => {
    const book = utils.book_new();
    book.Workbook = { WBProps: { date1904 } };
    utils.book_append_sheet(book, {
      A1: { t: 'n', v: date1904 ? 1 : 45292, z: 'yyyy-mm-dd' },
      B1: { t: 'n', v: 0.125, z: '0.0%' },
      C1: { t: 'e', v: 7 },
      D1: { t: 'n', f: '1+2' },
      '!ref': 'A1:D1',
    }, 'Formatted');
    utils.book_append_sheet(book, {}, 'Empty');
    const data = await parseWorkbook(write(book, { type: 'array', bookType: 'xlsx', compression: true }));
    expect(data.sheets[0].data.rows).toEqual([[date1904 ? '1904-01-02' : '2024-01-01', '12.5%', '#DIV/0!', '']]);
    expect(data.sheets[1].data).toEqual({ rows: [], columns: 0 });
  });

  it('rejects declared row and sheet counts before constructing preview rows', async () => {
    const rows = utils.book_new();
    utils.book_append_sheet(rows, { A1: { t: 's', v: 'fixture' }, '!ref': 'A1:A100001' }, 'Rows');
    await expect(parseWorkbook(write(rows, { type: 'array', bookType: 'xlsx', compression: true }))).rejects.toThrow('Workbook limit');
    const sheets = utils.book_new();
    for (let index = 0; index < 101; index++) utils.book_append_sheet(sheets, {}, 'Sheet' + index);
    await expect(parseWorkbook(write(sheets, { type: 'array', bookType: 'xlsx', compression: true }))).rejects.toThrow('Workbook limit');
  });
  it('rejects malformed archives and forged expansion size before parsing', async () => {
    await expect(parseWorkbook(new ArrayBuffer(30))).rejects.toThrow('Invalid workbook');
    const buffer = fixture(true); const view = new DataView(buffer);
    const end = buffer.byteLength - 22; const directory = view.getUint32(end + 16, true);
    view.setUint32(directory + 24, 64 * 1024 * 1024 + 1, true);
    await expect(validateWorkbookArchive(buffer)).rejects.toThrow('Workbook limit');
    view.setUint32(directory + 24, 1, true);
    await expect(validateWorkbookArchive(buffer)).rejects.toThrow('Workbook limit');
  });
});
