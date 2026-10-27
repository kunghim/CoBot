import { expect, it } from 'vitest';
import { selectedChild } from './treeSelection';
it('reveals only the next selected segment missing from the loaded pages', () => {
  expect(selectedChild('', 'last/deep/note.md', ['first'])).toEqual({ name: 'last', path: 'last' });
  expect(selectedChild('last', 'last/deep/note.md', [])).toEqual({ name: 'deep', path: 'last/deep' });
  expect(selectedChild('last/deep', 'last/deep/note.md', [])).toEqual({ name: 'note.md', path: 'last/deep/note.md' });
  expect(selectedChild('', 'last/deep/note.md', ['last'])).toBeUndefined();
});
it('keeps directory boundaries and does not synthesize invalid path segments', () => {
  for (const [directory, selected] of [['src', 'src-other/file'], ['src', 'src'], ['', ''], ['', '../file'], ['', './file']]) expect(selectedChild(directory, selected, [])).toBeUndefined();
  expect(selectedChild('docs', 'docs/.hidden/note.md', [])).toEqual({ name: '.hidden', path: 'docs/.hidden' });
});
