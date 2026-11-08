import { describe, expect, it } from 'vitest';
import type { WorkspaceFile } from '../../api/types';
import { pickViewer, showsSourceButton, viewers } from './viewers';

// kind, file name, viewer as preview, viewer as source, whether the source button shows
const matrix: [string, string, string, string, boolean][] = [
  ['text', 'a.txt', 'source', 'source', false],
  ['markdown', 'a.md', 'markdown', 'source', true],
  ['html', 'a.html', 'html', 'source', true],
  ['svg', 'a.svg', 'image', 'source', true],
  ['image', 'a.png', 'image', 'image', false],
  ['pdf', 'a.pdf', 'pdf', 'pdf', false],
  ['audio', 'a.mp3', 'media', 'media', false],
  ['video', 'a.mp4', 'media', 'media', false],
  ['document', 'a.docx', 'document', 'document', false],
  ['spreadsheet', 'a.xlsx', 'workbook', 'workbook', false],
  ['spreadsheet', 'a.xlsm', 'workbook', 'workbook', false],
  ['spreadsheet', 'a.csv', 'table', 'source', true],
  ['spreadsheet', 'a.tsv', 'table', 'source', true],
  ['spreadsheet', 'A.CSV', 'table', 'source', true],
  ['presentation', 'a.pptx', 'presentation', 'presentation', false],
  ['notebook', 'a.ipynb', 'notebook', 'source', true],
  ['latex', 'a.tex', 'latex', 'latex', false],
  ['binary', 'a.bin', 'none', 'none', false],
  ['blocked', 'a.env', 'none', 'none', false],
  ['directory', 'a', 'none', 'none', false],
];

const file = (kind: string, name: string) => ({ kind, name }) as WorkspaceFile;

describe('which viewer shows a file', () => {
  it.each(matrix)('%s %s', (kind, name, preview, source, toggle) => {
    expect(pickViewer(file(kind, name), false)?.id ?? 'none').toBe(preview);
    expect(pickViewer(file(kind, name), true)?.id ?? 'none').toBe(source);
    expect(showsSourceButton(file(kind, name))).toBe(toggle);
  });

  it('covers every kind', () => {
    expect(new Set(matrix.map(([kind]) => kind))).toEqual(new Set(Object.keys(viewers)));
  });
});
