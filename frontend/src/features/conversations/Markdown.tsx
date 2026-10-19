// Markdown renders member replies the way Codex does: headings, lists, code,
// tables and links. Raw HTML in the text is never rendered (react-markdown
// escapes it). Links to local files are opened by the core, which only
// opens files inside the team folder; web links open in a new tab; any other
// scheme is dropped.
import { memo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { FolderOpen } from 'lucide-react';
import { api } from '../../api/client';
import { apiErrorMessage } from '../../i18n/errors';
import { linkTarget } from './links';

function Markdown({ text, teamId }: { text: string; teamId: string }) {
  const components: Components = {
    a: ({ href, children }) => <MarkdownLink href={href ?? ''} teamId={teamId}>{children}</MarkdownLink>,
    p: ({ children }) => <p className="my-1.5 first:mt-0 last:mb-0">{children}</p>,
    h1: ({ children }) => <h3 className="mb-1.5 mt-3 text-base font-semibold first:mt-0">{children}</h3>,
    h2: ({ children }) => <h3 className="mb-1.5 mt-3 text-base font-semibold first:mt-0">{children}</h3>,
    h3: ({ children }) => <h4 className="mb-1 mt-2.5 font-semibold first:mt-0">{children}</h4>,
    h4: ({ children }) => <h4 className="mb-1 mt-2 font-semibold first:mt-0">{children}</h4>,
    ul: ({ children }) => <ul className="my-1.5 list-disc space-y-0.5 pl-5">{children}</ul>,
    ol: ({ children }) => <ol className="my-1.5 list-decimal space-y-0.5 pl-5">{children}</ol>,
    blockquote: ({ children }) => <blockquote className="my-1.5 border-l-2 border-neutral-300 pl-3 text-neutral-600 dark:border-neutral-600 dark:text-neutral-300">{children}</blockquote>,
    hr: () => <hr className="my-3 border-neutral-200 dark:border-neutral-700" />,
    pre: ({ children }) => <pre className="my-2 overflow-x-auto rounded-xl bg-neutral-900/[0.04] p-3 text-xs leading-5 text-neutral-800 dark:bg-black/30 dark:text-neutral-100">{children}</pre>,
    code: ({ className, children }) =>
      className ? <code className={className}>{children}</code> : <code className="rounded bg-neutral-200/70 px-1 py-0.5 font-mono text-[0.85em] dark:bg-neutral-700/70">{children}</code>,
    table: ({ children }) => <div className="my-2 overflow-x-auto"><table className="min-w-full border-collapse text-xs">{children}</table></div>,
    th: ({ children }) => <th className="border border-neutral-300 px-2 py-1 text-left font-semibold dark:border-neutral-600">{children}</th>,
    td: ({ children }) => <td className="border border-neutral-300 px-2 py-1 align-top dark:border-neutral-600">{children}</td>,
  };
  return (
    <div className="min-w-0 break-words">
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components} urlTransform={(url) => (linkTarget(url).kind === 'blocked' ? '' : url)}>
        {text}
      </ReactMarkdown>
    </div>
  );
}

export default memo(Markdown);

function MarkdownLink({ href, teamId, children }: { href: string; teamId: string; children: ReactNode }) {
  const { t } = useTranslation();
  const [error, setError] = useState('');
  const target = linkTarget(href);
  if (target.kind === 'web') {
    return <a href={href} target="_blank" rel="noopener noreferrer" className="text-signal-600 underline underline-offset-2 dark:text-signal-400">{children}</a>;
  }
  if (target.kind === 'blocked') return <span>{children}</span>;
  const open = (reveal: boolean) => {
    setError('');
    api.openTeamPath(teamId, target.path, reveal).catch((err) => setError(apiErrorMessage(t, err)));
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
