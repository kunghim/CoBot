import { useTranslation } from 'react-i18next';
import type { ChatRun, ChatStep } from '../../api/types';
import { errorMessage } from '../../i18n/errors';
import Markdown from '../conversations/Markdown';
import { describeStep } from '../conversations/steps';
import { isChatRunning } from './state';

export default function ChatProcess({ run, chatId }: { run: ChatRun; chatId: string }) {
  const { t } = useTranslation();
  return <div className="space-y-2 text-xs text-neutral-500">
    <p role={isChatRunning(run.status) ? 'status' : undefined}>{t(`chat.status.${run.status}`, { defaultValue: run.status })}</p>
    {!!run.steps?.length && <details open={isChatRunning(run.status) || undefined}><summary className="cursor-pointer">{t('chat.process', { count: run.steps.length })}</summary><ol className="mt-2 space-y-2 border-l border-neutral-200 pl-3 dark:border-neutral-700">
      {run.steps.map((step) => <li key={step.id}><ChatStepView step={step} chatId={chatId} /></li>)}
    </ol></details>}
    {run.errorKey && <p className="text-red-600 dark:text-red-400">{errorMessage(t, run.errorKey)}</p>}
  </div>;
}
function ChatStepView({ step, chatId }: { step: ChatStep; chatId: string }) {
  const { t } = useTranslation();
  const detail = (step.detail ?? {}) as Record<string, unknown>;
  const phrase = describeStep(step, (id) => id);
  const text = typeof detail.text === 'string' ? detail.text : typeof detail.command === 'string' ? detail.command : typeof detail.query === 'string' ? detail.query : '';
  return <details><summary className="cursor-pointer">{t(phrase.key, phrase.params)}{text ? ` · ${text.slice(0, 100)}` : ''}</summary>
    <div className="mt-2 overflow-auto rounded-lg bg-sunk p-3">{step.kind === 'message' ? <Markdown text={text} chatId={chatId} /> : <pre className="max-h-72 whitespace-pre-wrap break-all text-xs">{JSON.stringify(detail, null, 2)}</pre>}</div>
  </details>;
}
