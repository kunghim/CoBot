import LoadMore from '../../components/LoadMore';
// QuestionList surfaces the ask_user questions the user still owes an answer
// to, the route stages waiting for the user's confirmation, and the finished
// requirements waiting for the user's acceptance. An
// answer is ordinary input delivered to the asking bot; it is never a security
// authorization, and the panel says so.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAnswerQuestion, useCancelQuestion, useQuestions } from '../../api/queries';
import type { Question } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';
import { Alert, Button, Checkbox, Count, Icons, Skeleton, TextInput } from '../../components/ui';
import Markdown from '../conversations/Markdown';

export default function QuestionList({ teamId, viewBotId }: { teamId: string; viewBotId?: string }) {
  const { t } = useTranslation();
  const questions = useQuestions(teamId, 'open', true, viewBotId);

  if (questions.isError && !questions.data) {
    return (
      <div className="px-3 pt-3">
        <Alert tone="danger">{t('questions.loadFailed')}</Alert>
      </div>
    );
  }
  if (questions.isPending) {
    return (
      <div className="px-3 pt-3" role="status" aria-label={t('status.loading')}>
        <Skeleton className="h-8 w-full" />
      </div>
    );
  }
  const items = questions.data?.items ?? [];
  if (items.length === 0) return null;

  return (
    <section aria-label={t('questions.title')} className="border-b border-neutral-200 dark:border-neutral-800">
      <p className="flex items-center gap-1.5 px-3 pb-2 pt-3 text-xs font-semibold text-neutral-700 dark:text-neutral-200">
        <Icons.help size={14} aria-hidden />
        {t('questions.title')}
        <Count value={items.length} label={t('questions.title')} />
      </p>
      <ul className="space-y-2 px-3 pb-3">
        {items.map((question) =>
          question.kind === 'acceptance' || question.kind === 'confirm' ? (
            <AcceptanceCard key={question.id} question={question} />
          ) : (
            <QuestionCard key={question.id} question={question} />
          ),
        )}
      </ul>
      <LoadMore page={questions} />
    </section>
  );
}

const cardClass = 'rounded-xl bg-card p-3 text-sm shadow-sm ring-1 ring-neutral-900/[0.06] dark:ring-white/[0.08]';

function QuestionCard({ question }: { question: Question }) {
  const { t } = useTranslation();
  const answer = useAnswerQuestion();
  const cancel = useCancelQuestion(question.teamId);
  const [text, setText] = useState('');
  const [errorText, setErrorText] = useState('');

  const send = (answerText: string) => {
    if (!answerText.trim()) return;
    setErrorText('');
    answer.mutate(
      { questionId: question.id, answerText: answerText.trim(), expectedRevision: question.revision },
      { onSuccess: () => setText(''), onError: (error) => setErrorText(apiErrorMessage(t, error)) },
    );
  };

  return (
    <li className={cardClass}>
      <p className="whitespace-pre-wrap font-medium leading-6 text-neutral-900 dark:text-neutral-50">{question.question}</p>
      <p className="mt-1 text-2xs leading-4 text-neutral-500 dark:text-neutral-400">{t('questions.answerIsInput')}</p>
      {(question.options ?? []).length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {(question.options ?? []).map((option) => (
            <Button key={option} size="sm" shape="pill" onClick={() => send(option)} disabled={answer.isPending}>
              {option}
            </Button>
          ))}
        </div>
      )}
      <form
        className="mt-2 flex gap-1.5"
        onSubmit={(event) => {
          event.preventDefault();
          send(text);
        }}
      >
        <TextInput size="sm" className="min-w-0 flex-1" value={text} onChange={(event) => setText(event.target.value)} placeholder={t('questions.answerPlaceholder')} aria-label={t('questions.answerPlaceholder')} />
        <Button type="submit" size="sm" variant="primary" disabled={!text.trim()} loading={answer.isPending}>
          {t('questions.answerSend')}
        </Button>
      </form>
      <div className="mt-1.5 flex justify-end">
        <Button
          size="sm"
          variant="ghost"
          onClick={() => {
            setErrorText('');
            cancel.mutate(
              { questionId: question.id, expectedRevision: question.revision },
              { onError: (error) => setErrorText(apiErrorMessage(t, error)) },
            );
          }}
          loading={cancel.isPending}
        >
          {t('questions.cancel')}
        </Button>
      </div>
      {errorText && <Alert tone="danger">{errorText}</Alert>}
    </li>
  );
}

// AcceptanceCard puts a decision to the user: a confirm stage of a route (the
// requirement document, a design) or the finished requirement with the
// project manager's report. The user accepts, or asks for changes, which
// must say what.
function AcceptanceCard({ question }: { question: Question }) {
  const { t } = useTranslation();
  const answer = useAnswerQuestion();
  const [changes, setChanges] = useState('');
  const [errorText, setErrorText] = useState('');
  // A confirmation's changes run the reviews again unless the user only
  // wants to see the fix itself.
  const [returnToMe, setReturnToMe] = useState(false);
  const kind = question.kind === 'confirm' ? 'confirm' : 'acceptance';

  const decide = (decision: 'accept' | 'revise') => {
    if (decision === 'revise' && !changes.trim()) return;
    setErrorText('');
    answer.mutate(
      {
        questionId: question.id,
        answerText: decision === 'revise' ? changes.trim() : '',
        decision,
        mode: decision === 'revise' && kind === 'confirm' && returnToMe ? 'return_to_me' : undefined,
        expectedRevision: question.revision,
      },
      { onSuccess: () => setChanges(''), onError: (error) => setErrorText(apiErrorMessage(t, error)) },
    );
  };

  return (
    <li className={cardClass}>
      <p className="flex items-center gap-1.5 text-xs font-semibold text-emerald-700 dark:text-emerald-400">
        <Icons.success size={14} aria-hidden />
        {t(`questions.${kind}.title`)}
      </p>
      <div className="mt-1.5 max-h-96 overflow-y-auto text-neutral-800 dark:text-neutral-200">
        <Markdown text={question.question} teamId={question.teamId} />
      </div>
      <form
        className="mt-2 flex gap-1.5"
        onSubmit={(event) => {
          event.preventDefault();
          decide('revise');
        }}
      >
        <TextInput size="sm" className="min-w-0 flex-1" value={changes} onChange={(event) => setChanges(event.target.value)} placeholder={t(`questions.${kind}.changesPlaceholder`)} aria-label={t(`questions.${kind}.changesPlaceholder`)} />
        <Button type="submit" size="sm" disabled={!changes.trim() || answer.isPending}>
          {t(`questions.${kind}.revise`)}
        </Button>
      </form>
      {kind === 'confirm' && (
        <div className="mt-2">
          <Checkbox checked={returnToMe} onChange={setReturnToMe} label={t('questions.confirm.returnToMe')} />
        </div>
      )}
      <div className="mt-2 flex justify-end">
        <Button size="sm" variant="primary" icon={Icons.done} onClick={() => decide('accept')} loading={answer.isPending}>
          {t(`questions.${kind}.accept`)}
        </Button>
      </div>
      {errorText && <Alert tone="danger">{errorText}</Alert>}
    </li>
  );
}