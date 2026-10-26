// A relay-posted document — the requirement to confirm, the acceptance
// report — reads where the conversation is, but as a document of its own:
// a framed card with a title line, not another reply bubble. It opens in
// full and folds to its title once read.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { cx, Icons, interactive } from '../../components/ui';
import Markdown from './Markdown';

/** Splits a document into its first heading and the body under it. */
function splitDocument(text: string): { title: string; body: string } {
  const lines = text.split('\n');
  if (lines[0]?.startsWith('# ')) {
    return { title: lines[0].slice(2).trim(), body: lines.slice(1).join('\n').replace(/^\s+/, '') };
  }
  return { title: '', body: text };
}

export default function DocumentCard({ text, teamId }: { text: string; teamId?: string }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(true);
  const { title, body } = splitDocument(text);
  return (
    <section className="my-2 overflow-hidden rounded-2xl border border-neutral-900/[0.08] bg-canvas shadow-sheet dark:border-white/[0.10]">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className={cx('flex w-full items-center gap-2 px-4 py-2.5 text-left text-sm', interactive.hover)}
      >
        <span className="shrink-0 text-neutral-400 dark:text-neutral-500"><Icons.view size={15} aria-hidden /></span>
        <span className="min-w-0 flex-1 truncate font-medium text-neutral-900 dark:text-neutral-50">{title || t('conversation.document.untitled')}</span>
        <span className="shrink-0 text-xs text-neutral-400 dark:text-neutral-500">{t('conversation.document.label')}</span>
        <span className="shrink-0 text-neutral-400 dark:text-neutral-500">{open ? <Icons.expand size={14} aria-hidden /> : <Icons.next size={14} aria-hidden />}</span>
      </button>
      {open && (
        <div className="max-h-96 overflow-y-auto border-t border-neutral-900/[0.06] px-4 py-3 text-sm leading-6 text-neutral-800 dark:border-white/[0.08] dark:text-neutral-100">
          <Markdown text={body} teamId={teamId} />
        </div>
      )}
    </section>
  );
}
