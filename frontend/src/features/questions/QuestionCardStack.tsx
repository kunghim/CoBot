// QuestionCardStack shows what the user still owes a decision on, one card at
// a time above the composer, where the approval cards are: a member's
// ask_user question, a confirm stage of a route (the requirement document, a
// design), and a finished requirement waiting for acceptance. These stop the
// flow until they are answered, so they are not left in a side panel the user
// may never open. The document to decide on is not in the card: it is in the
// conversation, above, where it was read; the card only asks and takes the
// answer. An answer is ordinary input delivered to the asking member; it is
// never a security authorization, and the card says so.
import { useContext, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAnswerQuestion, useCancelQuestion, useCancelRequirement, useQuestions } from '../../api/queries';
import type { Question } from '../../api/types';
import { Alert, Button, Checkbox, Icons, TextInput, WaitingCard } from '../../components/ui';
import { apiErrorMessage } from '../../i18n/errors';
import Avatar from '../avatars/Avatar';
import { clampPagerIndex } from '../approvals/pager';
import { TeamMembersContext } from '../conversations/ExchangeLine';
import Markdown from '../conversations/Markdown';

export default function QuestionCardStack({ teamId, viewBotId }: { teamId: string; viewBotId?: string }) {
  const { t } = useTranslation();
  const questions = useQuestions(teamId, 'open', true, viewBotId);
  const [index, setIndex] = useState(0);
  // An acceptance dismissed from its card comes back from this entry; a
  // cancelled confirmation never does.
  const [dismissed, setDismissed] = useState<ReadonlySet<string>>(new Set());

  const items = questions.data?.items ?? [];
  const visible = items.filter((question) => !dismissed.has(question.id));
  const current = clampPagerIndex(index, visible.length);
  const question = visible[current];
  if (questions.isError && !questions.data) return <Alert tone="danger">{t('questions.loadFailed')}</Alert>;
  const reopen = dismissed.size > 0 && (
    <div className="flex justify-end">
      <Button size="sm" shape="pill" variant="secondary" onClick={() => setDismissed(new Set())}>
        {t('questions.dismissed', { count: dismissed.size })}
      </Button>
    </div>
  );
  if (!question) {
    return reopen ?? null;
  }
  const total = visible.length + (questions.hasNextPage ? 1 : 0);

  const frame = {
    position: { index: current + 1, total },
    onPrevious: () => setIndex(current - 1),
    onNext: () => {
      if (current < visible.length - 1) setIndex(current + 1);
      else if (questions.hasNextPage) void questions.fetchNextPage().then((result) => { if (!result.isError) setIndex(current + 1); });
    },
    previousLabel: t('questions.prev'),
    nextLabel: t('questions.next'),
  };
  // A card starts from nothing when the question changes. A dismissed
  // acceptance keeps its reopen entry beside whatever else still waits.
  return question.kind === 'acceptance' || question.kind === 'confirm' ? (
    <>
      <DecisionCard
        key={question.id}
        question={question}
        frame={frame}
        onDismiss={question.kind === 'acceptance' ? () => setDismissed((prev) => new Set(prev).add(question.id)) : undefined}
      />
      {reopen}
    </>
  ) : (
    <>
      <AskCard key={question.id} question={question} frame={frame} />
      {reopen}
    </>
  );
}

interface Frame {
  position: { index: number; total: number };
  onPrevious: () => void;
  onNext: () => void;
  previousLabel: string;
  nextLabel: string;
}

// Asker renders the member that asked, for the card header's left side.
function Asker({ question }: { question: Question }) {
  const members = useContext(TeamMembersContext);
  const asker = members.get(question.requesterBotId);
  if (!asker) return null;
  return (
    <>
      <Avatar bot={asker} size="sm" />
      <span className="min-w-0 truncate">{asker.name}</span>
    </>
  );
}

// AskCard is an ask_user question: the options are answers, and so is the text.
function AskCard({ question, frame }: { question: Question; frame: Frame }) {
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
    <WaitingCard
      icon={<Icons.help size={16} />}
      title={t('questions.title')}
      initiator={<Asker question={question} />}
      {...frame}
      actions={
        <>
          <Button
            size="sm"
            shape="pill"
            variant="ghost"
            loading={cancel.isPending}
            onClick={() => {
              setErrorText('');
              cancel.mutate({ questionId: question.id, expectedRevision: question.revision }, { onError: (error) => setErrorText(apiErrorMessage(t, error)) });
            }}
          >
            {t('questions.cancel')}
          </Button>
          {(question.options ?? []).map((option) => (
            <Button key={option} size="sm" shape="pill" variant="secondary" onClick={() => send(option)} disabled={answer.isPending}>
              {option}
            </Button>
          ))}
          <Button size="sm" shape="pill" variant="primary" disabled={!text.trim()} loading={answer.isPending} onClick={() => send(text)}>
            {t('questions.answerSend')}
          </Button>
        </>
      }
    >
      <p className="whitespace-pre-wrap font-medium leading-6">{question.question}</p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          send(text);
        }}
      >
        <TextInput size="sm" value={text} onChange={(event) => setText(event.target.value)} placeholder={t('questions.answerPlaceholder')} aria-label={t('questions.answerPlaceholder')} />
      </form>
      <p className="text-2xs leading-4 text-neutral-500 dark:text-neutral-400">{t('questions.answerIsInput')}</p>
      {errorText && <Alert tone="danger">{errorText}</Alert>}
    </WaitingCard>
  );
}

// DecisionCard puts a decision to the user: a confirm stage of a route or the
// finished requirement. One main button follows the input: with something
// written it asks for changes, empty it confirms — Enter does the same.
// Ignoring steps the card aside: a confirmation is cancelled outright (talk
// first, the project manager submits anew), a finished requirement only
// waits, reopenable from the composer. What it is about is read in the
// conversation above.
function DecisionCard({ question, frame, onDismiss }: { question: Question; frame: Frame; onDismiss?: () => void }) {
  const { t } = useTranslation();
  const answer = useAnswerQuestion();
  const cancelRequirement = useCancelRequirement();
  const [changes, setChanges] = useState('');
  const [errorText, setErrorText] = useState('');
  // A confirmation's changes run the reviews again unless the user only
  // wants to see the fix itself.
  const [returnToMe, setReturnToMe] = useState(false);
  const kind = question.kind === 'confirm' ? 'confirm' : 'acceptance';
  const revising = changes.trim() !== '';

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

  const ignore = () => {
    setErrorText('');
    if (kind === 'confirm') {
      if (!question.requirementId) return;
      cancelRequirement.mutate(question.requirementId, { onError: (error) => setErrorText(apiErrorMessage(t, error)) });
      return;
    }
    onDismiss?.();
  };

  return (
    <WaitingCard
      icon={<Icons.success size={16} />}
      title={t(`questions.${kind}.title`)}
      initiator={<Asker question={question} />}
      {...frame}
      actions={
        <>
          <Button size="sm" shape="pill" variant="ghost" loading={kind === 'confirm' && cancelRequirement.isPending} onClick={ignore}>
            {t('questions.cancel')}
          </Button>
          <Button
            size="sm"
            shape="pill"
            variant="primary"
            icon={revising ? undefined : Icons.done}
            onClick={() => decide(revising ? 'revise' : 'accept')}
            loading={answer.isPending}
          >
            {t(revising ? `questions.${kind}.revise` : `questions.${kind}.accept`)}
          </Button>
        </>
      }
    >
      {/* A short line. A question made before the document moved into the conversation still holds the whole of it: that scrolls here. */}
      <div className="max-h-40 overflow-y-auto text-neutral-800 dark:text-neutral-200">
        <Markdown text={question.question} teamId={question.teamId} />
      </div>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          decide(revising ? 'revise' : 'accept');
        }}
      >
        <TextInput size="sm" value={changes} onChange={(event) => setChanges(event.target.value)} placeholder={t(`questions.${kind}.changesPlaceholder`)} aria-label={t(`questions.${kind}.changesPlaceholder`)} />
      </form>
      {kind === 'confirm' && question.canSkipChecks && <Checkbox checked={returnToMe} onChange={setReturnToMe} label={t('questions.confirm.returnToMe')} />}
      {errorText && <Alert tone="danger">{errorText}</Alert>}
    </WaitingCard>
  );
}
