import { describe, expect, it } from 'vitest';
import { relativeFile } from './relativeFile';

describe('document-relative files', () => {
  it('keeps relative references based on the document, including navigation suffixes', () => {
    expect(relativeFile('docs/guide/readme.md', '../image.svg')).toBe('docs/guide/../image.svg');
    expect(relativeFile('docs/readme.md', '../src/app.ts:12:4')).toBe('docs/../src/app.ts:12:4');
    expect(relativeFile('README.md', 'src/app.ts#L4')).toBe('src/app.ts#L4');
  });
  it('preserves absolute paths, file URIs and web links for the existing classifier', () => {
    for (const link of ['/project/a.ts', 'C:\\project\\a.ts', 'file:///project/a.ts', 'https://example.test/a', '#section']) {
      expect(relativeFile('docs/readme.md', link)).toBe(link);
    }
  });
});
