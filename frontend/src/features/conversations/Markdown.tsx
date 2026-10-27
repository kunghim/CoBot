// Markdown renders member replies the way Codex does: headings, lists, code,
// tables and links. Raw HTML in the text is never rendered (react-markdown
// escapes it). Links to local files are opened by the core, which only
// opens files inside the team folder; web links open in a new tab; any other
// scheme is dropped.
import { memo, useState, type ReactNode, isValidElement } from 'react';
import { CopyButton } from '../../components/ui';
import { useTranslation } from 'react-i18next';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { FolderOpen } from 'lucide-react';
import { api } from '../../api/client';
import { apiErrorMessage } from '../../i18n/errors';
import { linkTarget } from './links';
import { openWorkspaceFile } from '../workspace/fileNavigation';
import { relativeFile } from '../workspace/relativeFile';
import LocalImage from '../workspace/LocalImage';

function Markdown({ text, teamId, chatId, basePath = '', imageSources }: { text: string; teamId?: string; chatId?: string; basePath?: string; imageSources?: Record<string, string> }) {
  const components: Components = {
    img: ({ src, alt }) => {
      const attachment = src?.startsWith('attachment:') ? imageSources?.[src.slice('attachment:'.length)] : undefined;
      if (attachment && /^data:image\/(png|jpeg|gif|webp|svg\+xml)[;,]/i.test(attachment)) return <img src={attachment} alt={alt ?? ''} className="max-h-96 max-w-full rounded-lg object-contain" />;
      const path = relativeFile(basePath, src ?? '');
      const scope = chatId ? { kind: 'chat' as const, id: chatId } : teamId ? { kind: 'team' as const, id: teamId } : null;
      if (scope && linkTarget(path).kind === 'local') return <LocalImage scope={scope} path={path} alt={alt ?? ''} />;
      return /^https?:/i.test(path) ? <img src={path} alt={alt ?? ''} className="max-h-96 max-w-full rounded-lg object-contain" /> : <span>{alt}</span>;
    },
    a: ({ href, children }) => <MarkdownLink href={relativeFile(basePath, href ?? '')} teamId={teamId} chatId={chatId}>{children}</MarkdownLink>,
    p: ({ children }) => <p className="my-1.5 first:mt-0 last:mb-0">{children}</p>,
    h1: ({ children }) => <h3 className="mb-1.5 mt-3 text-base font-semibold first:mt-0">{children}</h3>,
    h2: ({ children }) => <h3 className="mb-1.5 mt-3 text-base font-semibold first:mt-0">{children}</h3>,
    h3: ({ children }) => <h4 className="mb-1 mt-2.5 font-semibold first:mt-0">{children}</h4>,
    h4: ({ children }) => <h4 className="mb-1 mt-2 font-semibold first:mt-0">{children}</h4>,
    ul: ({ children }) => <ul className="my-1.5 list-disc space-y-0.5 pl-5">{children}</ul>,
    ol: ({ children }) => <ol className="my-1.5 list-decimal space-y-0.5 pl-5">{children}</ol>,
    blockquote: ({ children }) => <blockquote className="my-1.5 border-l-2 border-neutral-300 pl-3 text-neutral-600 dark:border-neutral-600 dark:text-neutral-300">{children}</blockquote>,
    hr: () => <hr className="my-3 border-neutral-200 dark:border-neutral-700" />,
    pre: ({ children }) => <CodeBlock>{children}</CodeBlock>,
    code: ({ className, children }) =>
      className ? <code className={className}>{children}</code> : <code className="rounded bg-neutral-200/70 px-1 py-0.5 font-mono text-[0.85em] dark:bg-neutral-700/70">{children}</code>,
    table: ({ children }) => <div className="my-2 overflow-x-auto"><table className="min-w-full border-collapse text-xs">{children}</table></div>,
    th: ({ children }) => <th className="border border-neutral-300 px-2 py-1 text-left font-semibold dark:border-neutral-600">{children}</th>,
    td: ({ children }) => <td className="border border-neutral-300 px-2 py-1 align-top dark:border-neutral-600">{children}</td>,
  };
  return (
    <div className="min-w-0 break-words">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components} urlTransform={(url, key) => (key === 'src' && url.startsWith('attachment:') && imageSources?.[url.slice(11)] ? url : linkTarget(url).kind === 'blocked' ? '' : url)}>
        {text}
      </ReactMarkdown>
    </div>
  );
}

export default memo(Markdown);

export function MarkdownLink({ href, teamId, chatId, children }: { href: string; teamId?: string; chatId?: string; children: ReactNode }) {
  const { t } = useTranslation();
  const [error, setError] = useState('');
  const target = linkTarget(href);
  if (target.kind === 'web') {
    return <a href={href} target="_blank" rel="noopener noreferrer" className="text-signal-600 underline underline-offset-2 dark:text-signal-400">{children}</a>;
  }
  if (target.kind === 'blocked') return <span>{children}</span>;
  const open = (reveal: boolean) => {
    setError('');
    const scope = chatId ? { kind: 'chat' as const, id: chatId } : teamId ? { kind: 'team' as const, id: teamId } : null;
    const opening = !scope ? Promise.reject(new Error('No workspace')) : !reveal ? openWorkspaceFile(scope, target.path) : scope.kind === 'chat' ? api.openChatPath(scope.id, target.path, true) : api.openTeamPath(scope.id, target.path, true);
    opening.catch((err) => setError(apiErrorMessage(t, err)));
  };
  return (
    <span className="group/link inline">
      <a
        href={href}
        onClick={(event) => {
          event.preventDefault();
          open(false);
        }}
        title={t('conversation.file.open')}
        className="text-signal-600 underline underline-offset-2 dark:text-signal-400"
      >
        {children}
      </a>
      <button
        type="button"
        onClick={() => open(true)}
        title={t('conversation.file.reveal')}
        aria-label={t('conversation.file.reveal')}
        className="ml-0.5 inline-flex align-middle text-neutral-400 opacity-0 transition-opacity hover:text-neutral-700 focus:opacity-100 group-hover/link:opacity-100 dark:hover:text-neutral-200"
      >
        <FolderOpen size={12} aria-hidden="true" />
      </button>
      {error && <span role="alert" className="ml-1 text-xs text-red-600 dark:text-red-400">{error}</span>}
    </span>
  );
}

function codeText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(codeText).join('');
  if (isValidElement<{ children?: ReactNode }>(node)) return codeText(node.props.children);
  return '';
}

function CodeBlock({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  return <div className="my-2 rounded-xl bg-neutral-900/[0.04] dark:bg-black/30">
    <div className="flex justify-end px-2 pt-1"><CopyButton value={codeText(children)} copyLabel={t('chat.copy')} copiedLabel={t('chat.copied')} /></div>
    <pre className="overflow-x-auto px-3 pb-3 font-mono text-xs leading-5 text-neutral-800 dark:text-neutral-100">{children}</pre>
  </div>;
}
