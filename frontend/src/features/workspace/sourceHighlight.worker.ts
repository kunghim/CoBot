import hljs from 'highlight.js';

self.onmessage = (event: MessageEvent<{ text: string; path: string }>) => {
  const { text, path } = event.data;
  if (text.length > 64 * 1024) return;
  const name = path.split('/').pop()?.toLowerCase() ?? '';
  const extension = name.includes('.') ? name.split('.').pop()! : name;
  const aliases: Record<string, string> = { tsx: 'typescript', jsx: 'javascript', vue: 'xml', svelte: 'xml', htm: 'xml', ipynb: 'json', mdx: 'markdown', dockerfile: 'dockerfile' };
  const language = aliases[extension] ?? extension;
  if (!hljs.getLanguage(language)) { self.postMessage(''); return; }
  try { self.postMessage(hljs.highlight(text, { language, ignoreIllegals: true }).value); }
  catch { self.postMessage(''); }
};
