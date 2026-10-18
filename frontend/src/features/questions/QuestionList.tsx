import LoadMore from '../../components/LoadMore';
// QuestionList surfaces the ask_user questions the user still owes an answer
// to, and the finished requirements waiting for the user's acceptance. An
// answer is ordinary input delivered to the asking bot; it is never a security
// authorization, and the panel says so.
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAnswerQuestion, useCancelQuestion, useQuestions } from '../../api/queries';
import type { Question } from '../../api/types';
import { apiErrorMessage } from '../../i18n/errors';

export default function QuestionList({ teamId, viewBotId }: { teamId: string; viewBotId?: string }) {
  const { t } = useTranslation();
  const questions = useQuestions(teamId, 'open', true, viewBotId);

  if (questions.isError && !questions.data) {
    return <p className="px-3 py-2 text-sm text-red-600" role="alert">{t('questions.loadFailed')}</p>;
  }
  if (questions.isPending) {
    return (
      <p className="px-3 py-1 text-[11px] text-neutral-400" role="status">
        {t('status.loading')}
      </p>
    );
  }
  const items = questions.data?.items ?? [];
  if (items.length === 0) return null;

  return (
    <section aria-label={t('questions.title')} className="border-b border-neutral-200 dark:border-neutral-800">
      <p className="px-3 pb-1 pt-2 text-xs font-semibold text-neutral-500 dark:text-neutral-400">
        {t('questions.title')} · {items.length}
      </p>
      <ul className="space-y-2 px-3 pb-2">
        {items.map((question) =>
          question.kind === 'acceptance' ? (
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
    <li className="rounded-lg border border-neutral-200 p-2 text-sm dark:border-neutral-700">
      <p className="whitespace-pre-wrap text-neutral-800 dark:text-neutral-200">{question.question}</p>
      <p className="mt-1 text-[11px] text-neutral-400">{t('questions.answerIsInput')}</p>
      {(question.options ?? []).length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-1">
          {(question.options ?? []).map((option) => (
            <button
              key={option}
              type="button"
              onClick={() => send(option)}
              disabled={answer.isPending}
              className="rounded-full border border-neutral-300 px-2 py-0.5 text-xs hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-600 dark:hover:bg-neutral-800"
            >
              {option}
            </button>
          ))}
        </div>
      )}
      <form
        className="mt-1.5 flex gap-1"
        onSubmit={(event) => {
          event.preventDefault();
          send(text);
        }}
      >
        <input
          value={text}
          onChange={(event) => setText(event.target.value)}
          placeholder={t('questions.answerPlaceholder')}
          aria-label={t('questions.answerPlaceholder')}
          className="min-w-0 flex-1 rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
        />
        <button
          type="submit"
          disabled={!text.trim() || answer.isPending}
          className="rounded-md bg-neutral-900 px-2 py-1 text-xs font-medium text-white hover:bg-neutral-700 disabled:opacity-50 dark:bg-neutral-100 dark:text-neutral-900 dark:hover:bg-neutral-300"
        >
          {t('questions.answerSend')}
        </button>
      </form>
      <button
        type="button"
        onClick={() => {
          setErrorText('');
          cancel.mutate(
            { questionId: question.id, expectedRevision: question.revision },
            { onError: (error) => setErrorText(apiErrorMessage(t, error)) },
          );
        }}
        disabled={cancel.isPending}
        className="mt-1 text-xs text-neutral-400 underline-offset-2 hover:underline disabled:opacity-50"
      >
        {t('questions.cancel')}
      </button>
      {errorText && (
        <p className="mt-1 text-xs text-red-600" role="alert">
          {errorText}
        </p>
      )}
    </li>
  );
}

// AcceptanceCard hands a finished requirement to the user: the leader's
// delivery report, then accept, or ask for changes, which must say what.
function AcceptanceCard({ question }: { question: Question }) {
  const { t } = useTranslation();
  const answer = useAnswerQuestion();
  const [changes, setChanges] = useState('');
  const [errorText, setErrorText] = useState('');

  const decide = (decision: 'accept' | 'revise') => {
    if (decision === 'revise' && !changes.trim()) return;
    setErrorText('');
    answer.mutate(
      { questionId: question.id, answerText: decision === 'revise' ? changes.trim() : '', decision, expectedRevision: question.revision },
      { onSuccess: () => setChanges(''), onError: (error) => setErrorText(apiErrorMessage(t, error)) },
    );
  };

  return (
    <li className="rounded-lg border border-emerald-300 p-2 text-sm dark:border-emerald-700">
      <p className="text-xs font-semibold text-emerald-700 dark:text-emerald-400">{t('questions.acceptance.title')}</p>
      <p className="mt-1 whitespace-pre-wrap text-neutral-800 dark:text-neutral-200">{question.question}</p>
      <div className="mt-1.5 flex flex-wrap gap-1">
        <button
          type="button"
          onClick={() => decide('accept')}
          disabled={answer.isPending}
          className="rounded-md bg-emerald-600 px-2 py-1 text-xs font-medium text-white hover:bg-emerald-500 disabled:opacity-50"
        >
          {t('questions.acceptance.accept')}
        </button>
      </div>
      <form
        className="mt-1.5 flex gap-1"
        onSubmit={(event) => {
          event.preventDefault();
          decide('revise');
        }}
      >
        <input
          value={changes}
          onChange={(event) => setChanges(event.target.value)}
          placeholder={t('questions.acceptance.changesPlaceholder')}
          aria-label={t('questions.acceptance.changesPlaceholder')}
          className="min-w-0 flex-1 rounded-md border border-neutral-300 bg-white px-2 py-1 text-xs focus:border-neutral-500 focus:outline-none dark:border-neutral-600 dark:bg-neutral-800 dark:text-neutral-100"
        />
        <button
          type="submit"
          disabled={!changes.trim() || answer.isPending}
          className="rounded-md border border-neutral-300 px-2 py-1 text-xs font-medium hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-600 dark:hover:bg-neutral-800"
        >
          {t('questions.acceptance.revise')}
        </button>
      </form>
      {errorText && (
        <p className="mt-1 text-xs text-red-600" role="alert">
          {errorText}
        </p>
      )}
    </li>
  );
}