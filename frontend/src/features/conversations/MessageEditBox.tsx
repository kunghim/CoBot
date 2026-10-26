// The box that edits a message before it is sent again: the words as they
// were, the reason file changes will not follow, and the two buttons.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';

export default function MessageEditBox({ initial, onSave, onCancel }: { initial: string; onSave: (text: string) => Promise<unknown>; onCancel: () => void }) {
  const { t } = useTranslation();
  const [text, setText] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const save = async () => {
    if (!text.trim() || saving) return;
    setSaving(true);
    setError('');
    try {
      await onSave(text.trim());
    } catch (e) {
      setError(apiErrorMessage(t, e));
      setSaving(false);
    }
  };
  return (
    <div className="space-y-2">
      <textarea
        autoFocus
        value={text}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) void save();
          if (event.key === 'Escape') onCancel();
        }}
        rows={Math.min(12, Math.max(3, text.split('\n').length))}
        className="w-full resize-y rounded-xl border border-neutral-300 bg-white px-3 py-2 text-sm leading-6 text-neutral-900 outline-none focus:border-signal-500 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-50"
      />
      {error && (
        <p role="alert" className="text-xs text-red-600 dark:text-red-400">
          {error}
        </p>
      )}
      <p className="text-xs leading-5 text-neutral-500 dark:text-neutral-400">{t('conversation.message.hint')}</p>
      <div className="flex items-center gap-2">
        <Button size="sm" variant="primary" loading={saving} onClick={() => void save()}>
          {t('conversation.message.save')}
        </Button>
        <Button size="sm" variant="ghost" onClick={onCancel}>
          {t('common.cancel')}
        </Button>
      </div>
    </div>
  );
}
