import { read, utils } from 'xlsx';
import type { TableData } from './delimited';
import { validateWorkbookArchive } from './workbookArchive';
export type WorkbookData = { sheets: Array<{ name: string; data: TableData }> };

export async function parseWorkbook(buffer: ArrayBuffer): Promise<WorkbookData> {
  await validateWorkbookArchive(buffer);
  const book = read(buffer, { type: 'array', cellHTML: false, cellFormula: false, cellStyles: false, bookVBA: false, bookDeps: false, dense: true });
  if (book.SheetNames.length > 100) throw new Error('Workbook limit');
  let cells = 0;
  const sheets = book.SheetNames.map((name) => {
    const sheet = book.Sheets[name];
    const range = sheet['!ref'] ? utils.decode_range(sheet['!ref']) : undefined;
    const columns = range ? range.e.c + 1 : 0;
    const count = range ? range.e.r + 1 : 0;
    cells += columns * count;
    if (columns > 16384 || count > 100000 || cells > 1000000) throw new Error('Workbook limit');
    const rows: string[][] = [];
    for (let row = 0; row < count; row++) {
      const values: string[] = [];
      for (let column = 0; column < columns; column++) {
        const cell = sheet['!data']?.[row]?.[column];
        values.push(cell ? utils.format_cell(cell) ?? '' : '');
      }
      rows.push(values);
    }
    return { name, data: { rows, columns } };
  });
  return { sheets };
}
