import { expect, it } from 'vitest';
import { presentationFrame } from './presentationFrame';
it('keeps SVG geometry, internal references and static text but strips active and remote content', () => {
  const html = presentationFrame('<svg><defs><linearGradient id="color" /></defs><rect fill="url(#color)"/><use href="#shape"/><image href="https://example.invalid/image" onload="bad()"/><a href="javascript:bad()"><text>safe</text></a><foreignObject><div>text<script>bad()</script><iframe srcdoc="bad"></iframe></div></foreignObject></svg>');
  const document = new DOMParser().parseFromString(html, 'text/html');
  expect(document.querySelectorAll('script,iframe')).toHaveLength(0);
  expect(document.querySelector('image')?.hasAttribute('href')).toBe(false);
  expect(document.querySelector('image')?.hasAttribute('onload')).toBe(false);
  expect(document.querySelector('a')?.hasAttribute('href')).toBe(false);
  expect(document.querySelector('use')?.getAttribute('href')).toBe('#shape');
  expect(document.querySelector('rect')?.getAttribute('fill')).toBe('url(#color)');
  expect(document.body.textContent).toContain('safetext');
  expect(document.querySelector('meta')?.content).toContain("default-src 'none'");
});
