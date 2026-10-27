export type HTMLResource = (path: string, kind: 'image' | 'style' | 'font') => Promise<string>;

function resourcePath(base: string, reference: string): string | undefined {
  const value = reference.trim();
  if (!value || value.startsWith('#') || /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(value)) return undefined;
  const path = value.split(/[?#]/, 1)[0];
  return path.startsWith('/') ? path.slice(1) : [...base.split('/').slice(0, -1), path].join('/');
}

// Scan CSS strings and comments so quoted text containing "url(...)" stays text.
export async function rewriteCSS(css: string, base: string, load: HTMLResource, depth = 0): Promise<string> {
  let result = '';
  for (let index = 0; index < css.length;) {
    if (css.startsWith('/*', index)) {
      const end = css.indexOf('*/', index + 2);
      index = end < 0 ? css.length : end + 2;
      continue;
    }
    const char = css[index];
    if (char === '"' || char === "'") {
      const start = index++;
      while (index < css.length) { if (css[index] === '\\') index += 2; else if (css[index++] === char) break; }
      result += css.slice(start, index);
      continue;
    }
    const importRule = css.slice(index).match(/^@import\s+(?:url\(\s*)?(?:"([^"\n]*)"|'([^'\n]*)'|([^\s;)]+))\s*\)?\s*([^;]*);/i);
    if (importRule) {
      const path = resourcePath(base, importRule[1] ?? importRule[2] ?? importRule[3]);
      if (path && depth < 4) {
        try {
          const imported = await rewriteCSS(await load(path, 'style'), path, load, depth + 1);
          result += importRule[4].trim() ? `@media ${importRule[4]}{${imported}}` : imported;
        } catch { /* Missing or out-of-scope resources do not prevent the document preview. */ }
      }
      index += importRule[0].length;
      continue;
    }
    const url = css.slice(index).match(/^url\(\s*(?:"((?:\\.|[^"\\])*)"|'((?:\\.|[^'\\])*)'|([^)]*))\s*\)/i);
    if (url) {
      const reference = (url[1] ?? url[2] ?? url[3]).trim();
      const path = resourcePath(base, reference);
      let resolved = /^data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+$/i.test(reference) ? reference : '';
      if (path) {
        try { resolved = await load(path, /\.(?:woff2?|ttf|otf)(?:[?#]|$)/i.test(path) ? 'font' : 'image'); } catch { /* See above. */ }
      }
      result += `url("${resolved.replace(/["\\\r\n]/g, '')}")`;
      index += url[0].length;
      continue;
    }
    result += char;
    index++;
  }
  return result;
}

export async function prepareHTML(html: string, base: string, load: HTMLResource): Promise<string> {
  // Template contents remain inert while resources are validated and rewritten.
  const template = document.createElement('template');
  template.innerHTML = html.replace(/<(\/?)(html|head|body)(?=[\s>])/gi, '<$1cobot-preview-$2');
  template.content.querySelectorAll('script,iframe,object,embed,form,base,meta,noscript,video,audio,source,track').forEach((node) => node.remove());
  for (const node of template.content.querySelectorAll('*')) {
    const reference = node.getAttribute(node.tagName === 'LINK' ? 'href' : 'src') ?? '';
    const stylesheet = node.tagName === 'LINK' && node.getAttribute('rel')?.toLowerCase() === 'stylesheet';
    for (const attribute of [...node.attributes]) {
      if (/^on/i.test(attribute.name) || ['src', 'srcset', 'href', 'xlink:href', 'action', 'formaction', 'srcdoc', 'poster', 'background'].includes(attribute.name.toLowerCase())) node.removeAttribute(attribute.name);
    }
    if (node.hasAttribute('style')) node.setAttribute('style', await rewriteCSS(node.getAttribute('style')!, base, load));
    if (node.tagName === 'STYLE') node.textContent = (await rewriteCSS(node.textContent ?? '', base, load)).replace(/</g, '\\3c ');
    const path = resourcePath(base, reference);
    if (stylesheet) {
      try {
        if (path) { const style = document.createElement('style'); style.textContent = (await rewriteCSS(await load(path, 'style'), path, load)).replace(/</g, '\\3c '); node.replaceWith(style); }
        else node.remove();
      } catch { node.remove(); }
    } else if (node.tagName === 'IMG') {
      if (/^data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/=]+$/i.test(reference)) node.setAttribute('src', reference);
      else if (path) { try { node.setAttribute('src', await load(path, 'image')); } catch { /* Keep accessible alternative text. */ } }
    } else if (node.tagName === 'LINK') node.remove();
  }
  const head = template.content.querySelector('cobot-preview-head');
  const body = template.content.querySelector('cobot-preview-body');
  const root = template.content.querySelector('cobot-preview-html');
  const bodyElement = document.createElement('body');
  const htmlElement = document.createElement('html');
  for (const attribute of body?.attributes ?? []) bodyElement.setAttribute(attribute.name, attribute.value);
  for (const attribute of root?.attributes ?? []) htmlElement.setAttribute(attribute.name, attribute.value);
  const bodyHTML = bodyElement.outerHTML.replace('</body>', () => `${body?.innerHTML ?? template.innerHTML}</body>`);
  const headHTML = `<head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src data:; form-action 'none'; base-uri 'none'">${head?.innerHTML ?? ''}</head>`;
  return '<!doctype html>' + htmlElement.outerHTML.replace('</html>', () => headHTML + bodyHTML + '</html>');
}
