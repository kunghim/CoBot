/** Keep SVG layout and foreignObject text while disabling active content. */
export function presentationFrame(svg: string): string {
  const template = document.createElement('template');
  template.innerHTML = svg;
  template.content.querySelectorAll('script,iframe,object,embed,form,base,meta,link,audio,video').forEach((node) => node.remove());
  template.content.querySelectorAll('*').forEach((node) => {
    for (const attribute of [...node.attributes]) {
      const name = attribute.name.toLowerCase();
      if (name.startsWith('on') || ['action', 'formaction', 'srcdoc', 'srcset'].includes(name) || ['href', 'xlink:href', 'src'].includes(name) && !(node.localName !== 'a' && (attribute.value.startsWith('#') || /^data:image\/(?:png|jpeg|gif|webp|svg\+xml);base64,/i.test(attribute.value)))) node.removeAttribute(attribute.name);
    }
  });
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src 'none'; form-action 'none'; base-uri 'none'"><style>html,body{margin:0;width:100%;height:100%;overflow:hidden;color-scheme:light}body>svg{display:block;width:100%;height:100%}</style></head><body>${template.innerHTML}</body></html>`;
}
