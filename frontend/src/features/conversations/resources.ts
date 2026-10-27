import { fromMarkdown } from 'mdast-util-from-markdown';
import { linkTarget } from './links';

export type ReplyResource = { kind: 'file' | 'site'; target: string };
type Node = { type: string; children?: Node[]; url?: string; identifier?: string; value?: string };

export function localAddress(value: string): string | undefined {
  try {
    const url = new URL(value);
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return;
    if (url.hostname === 'localhost' || url.hostname.endsWith('.localhost') || url.hostname === '[::1]' || /^127\.[0-9]+\.[0-9]+\.[0-9]+$/.test(url.hostname)) return url.href;
  } catch { /* Not an address. */ }
}

function obviousPath(value: string): boolean {
  return !/[\r\n]/.test(value) && linkTarget(value).kind === 'local' && (/^(?:\.{1,2}[\\/]|[\\/]|[a-z]:[\\/]|file:)/i.test(value) || /[\\/]/.test(value) || /^[\p{L}\p{N}_@.-]+\.[\p{L}\p{N}_-]+(?::[0-9]+(?::[0-9]+)?)?$/u.test(value) || /^(?:Dockerfile|Makefile|LICENSE|README)$/i.test(value) || /^\.[\p{L}\p{N}_.-]+$/u.test(value));
}

/** Only explicit Markdown references and path-like text become candidates.
 * The server must confirm a file before a resource card is displayed. */
export function replyResources(text: string): ReplyResource[] {
  const root = fromMarkdown(text) as Node;
  const definitions = new Map<string, string>();
  const output: ReplyResource[] = [];
  const seen = new Set<string>();
  const add = (target: string, explicit = false) => {
    const site = localAddress(target);
    const kind = site ? 'site' : 'file';
    if (!site && (linkTarget(target).kind !== 'local' || !explicit && !obviousPath(target))) return;
    const value = site ?? target;
    const key = `${kind}:${value}`;
    if (!seen.has(key)) { seen.add(key); output.push({ kind, target: value }); }
  };
  const index = (node: Node) => {
    if (node.type === 'definition' && node.identifier && node.url) definitions.set(node.identifier.toLowerCase(), node.url);
    node.children?.forEach(index);
  };
  index(root);
  const visit = (node: Node) => {
    if (['code', 'html', 'definition'].includes(node.type)) return;
    if (node.type === 'link' || node.type === 'image') { if (node.url) add(node.url, true); return; }
    if (node.type === 'linkReference' || node.type === 'imageReference') { const url = definitions.get(node.identifier?.toLowerCase() ?? ''); if (url) add(url, true); return; }
    if (node.type === 'inlineCode') { if (node.value) add(node.value); return; }
    if (node.type === 'text' && node.value) {
      const urls = /https?:\/\/[^\s<>"'`]+/gi;
      const rest = node.value.replace(urls, (url) => { add(url.replace(/[.,;!?，。；！）)]+$/, '')); return ' '; });
      const paths = /(?:file:\/\/[^\s<>"'`]+|(?:\/?[a-z]:[\\/]|\.{1,2}[\\/]|\/)[^\s<>"'`,;!?，。；！）)]+|[\p{L}_@][\p{L}\p{N}_@.\-]*[\\/][^\s<>"'`,;!?，。；！）)]+|[\p{L}_@][\p{L}\p{N}_@.\-]*\.[a-z][a-z0-9_-]*(?::[0-9]+(?::[0-9]+)?)?)/giu;
      for (const match of rest.matchAll(paths)) add(match[0].replace(/[.,;]+$/, ''));
      for (const match of rest.matchAll(/(?:^|\s)(\.[a-z_][a-z0-9_.-]*|Dockerfile|Makefile|LICENSE|README)(?=$|[\s.,;])/gi)) add(match[1]);
    }
    node.children?.forEach(visit);
  };
  visit(root);
  return output;
}

/** A display de-duplication key; filesystem authority still belongs to core. */
export function resourcePathKey(path: string, directory = ''): string {
  const normalize = (value: string) => {
    if (/^file:/i.test(value)) { try { value = decodeURIComponent(new URL(value).pathname); } catch { /* Keep invalid URI as an unmatched candidate. */ } }
    value = value.replace(/\\/g, '/').replace(/^\/([a-z]:\/)/i, '$1');
    const parts: string[] = [];
    for (const part of value.split('/')) { if (part === '.' || part === '') continue; if (part === '..' && parts.length && parts[parts.length - 1] !== '..') parts.pop(); else parts.push(part); }
    return parts.join('/');
  };
  const absolute = /^(?:[\\/]|[a-z]:[\\/]|file:)/i.test(path);
  const root = normalize(directory);
  const value = normalize(path);
  return absolute && root && value.startsWith(root + '/') ? value.slice(root.length + 1) : absolute ? 'absolute:' + value : value;
}

export function changedPaths(steps: Array<{ kind: string; status: string; detail: Record<string, unknown> }>): string[] {
  const completed = steps.filter((step) => step.status === 'completed');
  const turn = completed.filter((step) => step.kind === 'diff' && step.detail.filesComplete === true && Array.isArray(step.detail.changes)).at(-1);
  const source = turn ? [turn] : completed.filter((step) => step.kind === 'file_change');
  const paths: string[] = [];
  for (const step of source) for (const value of Array.isArray(step.detail.changes) ? step.detail.changes : []) {
    if (value && typeof value === 'object') { const change = value as Record<string, unknown>; if (typeof change.path === 'string') paths.push(change.path); if (typeof change.movePath === 'string') paths.push(change.movePath); }
  }
  return paths;
}
