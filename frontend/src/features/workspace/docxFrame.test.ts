import { expect, it } from 'vitest';
import { docxFrame } from './docxFrame';
it('allows only the trusted inline renderer while denying network and application authority', () => {
  const html = docxFrame('const value = "</script><script>unexpected()</script>";', 'nonce-123');
  const document = new DOMParser().parseFromString(html, 'text/html');
  expect(document.querySelectorAll('script')).toHaveLength(1);
  expect(document.querySelector('script')?.getAttribute('nonce')).toBe('nonce-123');
  const policy = document.querySelector('meta')?.getAttribute('content');
  expect(policy).toContain("script-src 'nonce-nonce-123'");
  expect(policy).toContain("connect-src 'none'");
  expect(policy).toContain("form-action 'none'");
  expect(policy).not.toContain('unsafe-eval');
  expect(document.querySelector('script')?.textContent).toContain('<\\/script>');
  expect(() => docxFrame('', 'invalid"nonce')).toThrow('Invalid nonce');
});
