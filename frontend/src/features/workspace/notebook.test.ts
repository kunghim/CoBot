import { expect, it } from 'vitest';
import { inertHTML, parseNotebook } from './notebook';

it('reads cell-based documents without requiring a specific version marker', () => {
  const cells = [{ cell_type: 'raw', source: 'Saved cell' }];
  for (const marker of [{}, { nbformat: 3 }, { nbformat: 5 }]) {
    expect(parseNotebook(JSON.stringify({ ...marker, cells }))[0].source).toBe('Saved cell');
  }
  expect(() => parseNotebook(JSON.stringify({ nbformat: 3, worksheets: [] }))).toThrow('Invalid notebook');
});

it('reads multiline cell sources and chooses a single rich output representation', () => {
  const cells = parseNotebook(JSON.stringify({ nbformat: 4, cells: [
    { cell_type: 'markdown', source: ['# Title\n', 'body'] },
    { cell_type: 'code', source: 'print(1)', execution_count: 7, outputs: [
      { output_type: 'stream', text: ['one\n', 'two\n'] },
      { output_type: 'display_data', data: { 'image/png': 'YWJj', 'text/plain': 'duplicate' } },
      { output_type: 'execute_result', data: { 'text/html': '<table><tr><td>2</td></tr></table>', 'text/plain': 'duplicate' } },
    ] },
  ] }));
  expect(cells[0].source).toBe('# Title\nbody');
  expect(cells[1].execution).toBe(7);
  expect(cells[1].outputs.map((output) => output.kind)).toEqual(['text', 'image', 'html']);
  expect(cells[1].outputs[0].value).toBe('one\ntwo\n');
});

it('rejects invalid documents and bounds individual sources and outputs', () => {
  expect(() => parseNotebook('{}')).toThrow();
  expect(() => parseNotebook('{')).toThrow();
  const [cell] = parseNotebook(JSON.stringify({ nbformat: 4, cells: [{ cell_type: 'raw', source: 'a'.repeat(70_000), outputs: Array.from({ length: 101 }, () => ({ output_type: 'stream', text: 'b'.repeat(70_000) })) }] }));
  expect(cell.source.length).toBe(65_536);
  expect(cell.outputs.length).toBe(100);
  expect(cell.truncated).toBe(true);
  expect(cell.outputs.every((output) => output.truncated && output.value.length === 65_536)).toBe(true);
});

it('accepts the cell-count boundary and rejects the next cell before mapping outputs', () => {
  const cells = Array.from({ length: 10000 }, () => ({ cell_type: 'raw', source: '' }));
  expect(parseNotebook(JSON.stringify({ nbformat: 4, cells }))).toHaveLength(10000);
  cells.push({ cell_type: 'raw', source: '' });
  expect(() => parseNotebook(JSON.stringify({ nbformat: 4, cells }))).toThrow('Notebook has too many cells');
});

it('keeps embedded attachments as image data and excludes executable MIME types', () => {
  const [cell] = parseNotebook(JSON.stringify({ nbformat: 4, cells: [{ cell_type: 'markdown', source: '![plot](attachment:plot.svg)', attachments: {
    'plot.svg': { 'image/svg+xml': '<svg xmlns="http://www.w3.org/2000/svg" />' },
    'unsafe': { 'text/html': '<script>attack()</script>' },
  } }] }));
  expect(cell.attachments?.['plot.svg']).toContain('data:image/svg+xml;charset=utf-8,');
  expect(cell.attachments?.unsafe).toBeUndefined();
});

it('removes active document constructs and blocks scripts and remote resources with CSP', () => {
  const html = inertHTML('<script>parent.attack()</script><base href="https://example.com"><meta http-equiv="refresh" content="0;url=https://example.com"><iframe srcdoc="bad"></iframe><a href="https://example.com" onclick="attack()">link</a><img src="https://example.com/tracker" onerror="attack()"><table><tr><td>safe</td></tr></table>');
  const document = new DOMParser().parseFromString(html, 'text/html');
  expect(document.querySelectorAll('script,base,iframe,[onclick],[onerror],a[href]')).toHaveLength(0);
  expect(document.querySelector('meta')?.content).toContain("default-src 'none'");
  expect(document.querySelector('meta')?.content).toContain('img-src data:');
  expect(document.querySelector('td')?.textContent).toBe('safe');
});
