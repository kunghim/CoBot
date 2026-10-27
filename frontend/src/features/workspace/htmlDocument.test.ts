import { expect, it, vi } from 'vitest';
import { prepareHTML, rewriteCSS } from './htmlDocument';

it('preserves body attributes and styles while loading relative resources through the supplied scope', async () => {
  const load = vi.fn(async (_path: string, kind: string) => kind === 'style' ? '.card{background:url(../images/bg.png)}' : 'data:image/png;base64,YWJj');
  const html = await prepareHTML('<!doctype html><html lang="zh"><head><link rel="stylesheet" href="css/site.css"></head><body class="card"><h1>Title</h1><img src="images/a.png?rev=1"></body></html>', 'docs/index.html', load);
  const document = new DOMParser().parseFromString(html, 'text/html');
  expect(document.documentElement.lang).toBe('zh');
  expect(document.body.className).toBe('card');
  expect(document.querySelector('style')?.textContent).toContain('data:image/png;base64,YWJj');
  expect(document.querySelector('img')?.src).toBe('data:image/png;base64,YWJj');
  expect(load).toHaveBeenCalledWith('docs/css/site.css', 'style');
  expect(load).toHaveBeenCalledWith('docs/css/../images/bg.png', 'image');
  expect(load).toHaveBeenCalledWith('docs/images/a.png', 'image');
});

it('removes active content, navigation and remote resources without fetching them', async () => {
  const load = vi.fn(async () => { throw new Error('Denied'); });
  const html = await prepareHTML('<meta http-equiv="refresh" content="0;url=https://example.com"><script>alert(1)</script><form><img src="private.png"></form><a href="file:///etc/passwd" onclick="attack()">link</a><img src="https://example.com/image" srcset="remote 2x"><img src="../../outside.png"><style>.x{background:url(https://example.com/track)}</style>', 'docs/index.html', load);
  const document = new DOMParser().parseFromString(html, 'text/html');
  expect(document.querySelectorAll('script,form,[onclick],a[href],img[src],img[srcset]')).toHaveLength(0);
  expect(load.mock.calls).toEqual([['docs/../../outside.png', 'image']]);
  expect(document.querySelector('meta')?.content).toContain("default-src 'none'");
});

it('rewrites nested CSS imports with their own base, bounds cycles and preserves quoted text', async () => {
  const load = vi.fn(async (_path: string, kind: string) => kind === 'style' ? '@import "again.css"; .x{background:url("../p.png")}' : 'blob:scoped');
  const css = await rewriteCSS('/* url(secret) */ @import url("theme.css") screen; .x{content:"url(secret)";src:url(font.woff2)}', 'docs/style.css', load);
  expect(css).toContain('@media screen{');
  expect(css).toContain('content:"url(secret)"');
  expect(css).not.toContain('@import');
  expect(load.mock.calls.filter(([, kind]) => kind === 'style')).toHaveLength(4);
  expect(load).toHaveBeenCalledWith('docs/font.woff2', 'font');
});

it('keeps stylesheet content from closing its containing HTML element', async () => {
  const html = await prepareHTML('<link rel="stylesheet" href="x.css">', 'index.html', async () => 'a{content:"</style><img src=data:image/png;base64,YWJj>"}');
  const document = new DOMParser().parseFromString(html, 'text/html');
  expect(document.querySelectorAll('img')).toHaveLength(0);
  expect(document.querySelector('style')?.textContent).toContain('\\3c ');
});

it('preserves literal dollar replacement sequences in document content', async () => {
  const html = await prepareHTML('<html><body><p>$& $$ $\'</p></body></html>', 'index.html', vi.fn());
  const document = new DOMParser().parseFromString(html, 'text/html');
  expect(document.querySelector('p')?.textContent).toBe('$& $$ $\'');
});
