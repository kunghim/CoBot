// MemoryList is the 记忆 tab: the selected member's working methods and
// preferences, read-only except deletion (a tombstone; the same fact is
// never recreated automatically). Storing happens through the member's own
// MCP tool, so this page never fabricates memories on the member's behalf.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useDeleteMemory, useMemories } from '../../api/queries';
import { apiErrorMessage } from '../../i18n/errors';

export default function MemoryList({ botId }: { botId: string }) {
  const { t, i18n } = useTranslation();
  const memories = useMemories(botId);
  const remove = useDeleteMemory();
  const [errorText, setErrorText] = useState('');

  if (memories.isPending) {
    return <p className="px-3 py-2 text-sm text-neutral-500" role="status">{t('status.loading')}</p>;
  }
  if (memories.isError) {
    return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('memories.loadFailed')}</p>;
  }
  const items = memories.data?.items ?? [];
  if (items.length === 0) {
    return <p className="px-3 py-2 text-sm text-neutral-500 dark:text-neutral-400">{t('memories.empty')}</p>;
  }
  const formatTime = (iso: string) => new Date(iso).toLocaleString(i18n.language);

  return (
    <div>
      <p className="px-3 pb-1 pt-2 text-[11px] text-neutral-400">{t('memories.readOnlyHint')}</p>
      {errorText && (
        <p className="px-3 text-xs text-red-600" role="alert">
          {errorText}
        </p>
      )}
      <ul className="space-y-2 px-3 pb-3">
        {items.map((memory) => (
          <li key={memory.id} className="rounded-lg border border-neutral-200 p-2 text-sm dark:border-neutral-700">
            <p className="whitespace-pre-wrap break-words text-neutral-800 dark:text-neutral-200">{memory.body}</p>
            <div className="mt-1 flex items-center justify-between text-[11px] text-neutral-400">
              <span>{formatTime(memory.createdAt)}</span>
              <button
                type="button"
                onClick={() => {
                  setErrorText('');
                  remove.mutate(
                    { botId, memoryId: memory.id },
                    { onError: (error) => setErrorText(apiErrorMessage(t, error)) },
                  );
                }}
                disabled={remove.isPending}
                className="underline-offset-2 hover:underline disabled:opacity-50"
              >
                {t('memories.delete')}
              </button>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}
