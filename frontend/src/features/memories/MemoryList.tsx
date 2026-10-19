import LoadMore from '../../components/LoadMore';
// MemoryList is the 记忆 tab: the selected member's working methods and
// preferences, read-only except deletion (a tombstone; the same fact is
// never recreated automatically). Storing happens through the member's own
// MCP tool, so this page never fabricates memories on the member's behalf.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useDeleteMemory, useMemories } from '../../api/queries';
import { apiErrorMessage } from '../../i18n/errors';
import { Alert, Button, EmptyState, Icons, Skeleton } from '../../components/ui';

export default function MemoryList({ botId }: { botId: string }) {
  const { t, i18n } = useTranslation();
  const memories = useMemories(botId);
  const remove = useDeleteMemory();
  const [errorText, setErrorText] = useState('');

  if (memories.isPending) {
    return <div className="space-y-2 px-3 py-3" role="status" aria-label={t('status.loading')}><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>;
  }
  if (memories.isError && !memories.data) {
    return <div className="px-3 py-3"><Alert tone="danger">{t('memories.loadFailed')}</Alert></div>;
  }
  const items = memories.data?.items ?? [];
  if (items.length === 0) {
    return <EmptyState icon={<Icons.memory size={20} aria-hidden />} title={t('memories.empty')} />;
  }
  const formatTime = (iso: string) => new Date(iso).toLocaleString(i18n.language);

  return (
    <div>
      <p className="px-3 pb-2 pt-3 text-2xs leading-4 text-neutral-500 dark:text-neutral-400">{t('memories.readOnlyHint')}</p>
      {errorText && (
        <div className="px-3 pb-2"><Alert tone="danger">{errorText}</Alert></div>
      )}
      <ul className="space-y-2 px-3 pb-3">
        {items.map((memory) => (
          <li key={memory.id} className="rounded-xl bg-card p-3 text-sm shadow-sm ring-1 ring-neutral-900/[0.06] dark:ring-white/[0.08]">
            <p className="whitespace-pre-wrap break-words text-neutral-800 dark:text-neutral-200">{memory.body}</p>
            <div className="mt-1.5 flex items-center justify-between text-2xs text-neutral-500 dark:text-neutral-400">
              <span>{formatTime(memory.createdAt)}</span>
              <Button
                size="sm"
                variant="ghost"
                icon={Icons.delete}
                onClick={() => {
                  setErrorText('');
                  remove.mutate(
                    { botId, memoryId: memory.id },
                    { onError: (error) => setErrorText(apiErrorMessage(t, error)) },
                  );
                }}
                disabled={remove.isPending}
              >
                {t('memories.delete')}
              </Button>
            </div>
          </li>
        ))}
      </ul>
      <LoadMore page={memories} />
    </div>
  );
}
