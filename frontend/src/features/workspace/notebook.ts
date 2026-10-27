export type NotebookOutput = { kind: 'text' | 'markdown' | 'html' | 'image'; value: string; truncated?: boolean };
export type NotebookCell = { kind: 'code' | 'markdown' | 'raw'; source: string; execution: number | null; outputs: NotebookOutput[]; truncated: boolean; attachments?: Record<string, string> };
export const MAX_NOTEBOOK_BYTES = 10 * 1024 * 1024;
const MAX_TEXT = 64 * 1024;
const MAX_IMAGE = 2 * 1024 * 1024;

const record = (value: unknown): Record<string, unknown> => value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {};
const text = (value: unknown): string => typeof value === 'string' ? value : Array.isArray(value) && value.every((part) => typeof part === 'string') ? value.join('') : '';
const outputText = (kind: NotebookOutput['kind'], value: string): NotebookOutput => ({ kind, value: value.slice(0, MAX_TEXT), truncated: value.length > MAX_TEXT });

function dataImage(data: Record<string, unknown>): string | undefined {
  const mime = ['image/png', 'image/jpeg', 'image/gif', 'image/webp'].find((type) => typeof data[type] === 'string' || Array.isArray(data[type]));
  const encoded = mime ? text(data[mime]).replace(/\s/g, '') : '';
  if (mime && encoded && encoded.length <= MAX_IMAGE && /^[A-Za-z0-9+/]*={0,2}$/.test(encoded)) return `data:${mime};base64,${encoded}`;
  const svg = text(data['image/svg+xml']);
  if (svg && svg.length <= MAX_IMAGE) return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  return undefined;
}

export function parseNotebook(json: string): NotebookCell[] {
  const notebook = record(JSON.parse(json));
  if (!Array.isArray(notebook.cells)) throw new Error('Invalid notebook');
  if (notebook.cells.length > 10_000) throw new Error('Notebook has too many cells');
  return notebook.cells.map((value) => {
    const cell = record(value);
    if (!['code', 'markdown', 'raw'].includes(String(cell.cell_type))) throw new Error('Invalid cell');
    const source = text(cell.source);
    const allOutputs = Array.isArray(cell.outputs) ? cell.outputs : [];
    const outputs: NotebookOutput[] = [];
    for (const value of allOutputs.slice(0, 100)) {
      const output = record(value);
      if (output.output_type === 'stream') outputs.push(outputText('text', text(output.text)));
      else if (output.output_type === 'error') outputs.push(outputText('text', text(output.traceback) || `${text(output.ename)}: ${text(output.evalue)}`));
      else {
        const data = record(output.data);
        const image = dataImage(data);
        if (image) outputs.push({ kind: 'image', value: image });
        else if (data['text/html']) outputs.push(outputText('html', text(data['text/html'])));
        else if (data['text/markdown']) outputs.push(outputText('markdown', text(data['text/markdown'])));
        else if (data['text/plain']) outputs.push(outputText('text', text(data['text/plain'])));
        else if (Object.keys(data).some((key) => key.startsWith('image/'))) outputs.push({ kind: 'text', value: '', truncated: true });
      }
    }
    const attachments: Record<string, string> = Object.create(null);
    for (const [name, data] of Object.entries(record(cell.attachments)).slice(0, 100)) { const image = dataImage(record(data)); if (image) attachments[name] = image; }
    return { attachments, kind: cell.cell_type as NotebookCell['kind'], source: source.slice(0, MAX_TEXT), execution: typeof cell.execution_count === 'number' && Number.isFinite(cell.execution_count) ? cell.execution_count : null, outputs, truncated: source.length > MAX_TEXT || allOutputs.length > 100 };
  });
}

// No active navigation, nested documents, scripts, forms or remote subresources.
export function inertHTML(html: string): string {
  const template = document.createElement('template');
  template.innerHTML = html;
  template.content.querySelectorAll('script,iframe,object,embed,form,base,meta,link').forEach((node) => node.remove());
  template.content.querySelectorAll('*').forEach((node) => {
    for (const attribute of [...node.attributes]) {
      if (/^on/i.test(attribute.name) || ['href', 'xlink:href', 'action', 'formaction', 'srcdoc', 'srcset'].includes(attribute.name.toLowerCase()) || attribute.name.toLowerCase() === 'src' && !/^data:image\/(png|jpeg|gif|webp);base64,/i.test(attribute.value)) node.removeAttribute(attribute.name);
    }
  });
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src 'none'; form-action 'none'; base-uri 'none'"><style>body{font:14px system-ui;overflow-wrap:anywhere}img{max-width:100%}pre{white-space:pre-wrap}table{border-collapse:collapse}td,th{padding:4px;border:1px solid #888}</style></head><body>${template.innerHTML}</body></html>`;
}
