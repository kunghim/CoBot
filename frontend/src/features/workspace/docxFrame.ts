/** The only executable source is our bundled renderer. Document bytes arrive
 * separately through postMessage and never become inline script text. */
export function docxFrame(source: string, nonce: string): string {
  if (!/^[a-zA-Z0-9-]+$/.test(nonce)) throw new Error('Invalid nonce');
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'nonce-${nonce}'; style-src 'unsafe-inline'; img-src data:; font-src data:; connect-src 'none'; base-uri 'none'; form-action 'none'; object-src 'none'"><style>html{color-scheme:light}body{margin:0;background:#e5e5e5;color:#111}#document{width:max-content;min-width:100%}.docx-wrapper{padding:16px!important}section.docx{overflow-wrap:anywhere}</style></head><body><div id="document"></div><script nonce="${nonce}">${source.replace(/<\/script/gi, '<\\/script')}</script></body></html>`;
}
