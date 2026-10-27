export type SourceToken = { text: string; classes: string };

export function highlightLines(html: string): SourceToken[][] {
  if (html.length > 2 * 1024 * 1024) return [];
  const template = document.createElement('template');
  template.innerHTML = html;
  const lines: SourceToken[][] = [[]];
  let count = 0;
  const visit = (node: Node, classes: string) => {
    if (node.nodeType === Node.TEXT_NODE) {
      const parts = (node.textContent ?? '').split(/\r\n|\r|\n/);
      parts.forEach((text, index) => {
        if (index) lines.push([]);
        if (text) { if (++count > 10_000) throw new Error('Too many tokens'); lines[lines.length - 1].push({ text, classes }); }
      });
    } else if (node instanceof HTMLSpanElement || node.nodeType === Node.DOCUMENT_FRAGMENT_NODE) {
      const own = node instanceof HTMLSpanElement ? [...node.classList].filter((name) => /^hljs-[a-z0-9_-]+$/.test(name)).join(' ') : '';
      node.childNodes.forEach((child) => visit(child, [classes, own].filter(Boolean).join(' ')));
    }
  };
  try { visit(template.content, ''); return lines; } catch { return []; }
}
