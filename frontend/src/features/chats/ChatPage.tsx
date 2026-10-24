import { useEffect, useRef, useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { api, ApiError } from '../../api/client';
import type { ChatRequest } from '../../api/types';
import { forgetChatRun, useChatLiveRuns } from '../../api/live';
import { useEngine, useEngineModels, useProviders, useSettings } from '../../api/queries';
import { Button, ChatComposer, CopyButton, MessageBubble, ModelEffortPicker, TextInput, cx } from '../../components/ui';
import { ShellToggle } from '../../app/shell';
import { readLocal, writeLocal } from '../../app/mode';
import { apiErrorMessage } from '../../i18n/errors';
import Markdown from '../conversations/Markdown';
import NativeApprovalBody from '../approvals/NativeApprovalBody';
import { choiceKey, defaultChoice, engineDefaultModel, selectableModels } from '../providers/providerModels';
import { useProviderName } from '../providers/useProviderName';
import { useChat } from './queries';
import { isChatBlocked, isChatRunning } from './state';
import ChatProcess from './ChatProcess';
import ChatWelcome from './ChatWelcome';
import ChatMenu from './ChatMenu';

export default function ChatRoute() {
  const { chatId } = useParams();
  return <ChatPage key={chatId ?? 'new'} id={chatId} />;
}

function ChatPage({ id }: { id?: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const query = useChat(id);
  const live = useChatLiveRuns();
  const key = 'cobot.chat.draft.' + (id ?? 'new');
  const [draft, setDraft] = useState(() => readLocal(key));
  const scroll = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const [bottom, setBottom] = useState(true);
  const page = query.data?.pages[0];
  const chat = page?.chat;
  const messages = [...new Map(query.data?.pages.slice().reverse().flatMap((p) => p.messages).map((m) => [m.id, m]) ?? []).values()];
  const runs = new Map(query.data?.pages.slice().reverse().flatMap((p) => p.runs).map((r) => [r.id, r]));
  const active = page?.runs.find((run) => isChatBlocked(run.status));
  const pending = page?.requests.filter((req) => req.state === 'pending') ?? [];
  const settings = useSettings(true);
  const providers = useProviders();
  const engine = useEngine();
  const engineModels = useEngineModels(true);
  const providerName = useProviderName();
  const offered = selectableModels(providers.data?.providers, engineModels.data?.models, engine.data?.loggedIn ?? false, providerName);
  const initial = defaultChoice(settings.data, providers.data?.providers, engineDefaultModel(engineModels.data?.models), engine.data?.loggedIn ?? false);
  const [picked, setPicked] = useState<string | null>(null);
  const [effort, setEffort] = useState('');
  const choice = offered.find((m) => m.key === (picked ?? choiceKey(initial.providerId, initial.model)));
  const chosenModel = engineModels.data?.models.find((m) => (m.slug ?? m.id) === choice?.model) ?? engineModels.data?.models[0];
  const refresh = () => { void qc.invalidateQueries({ queryKey: ['chat', id] }); void qc.invalidateQueries({ queryKey: ['chats'] }); };
  const action = useMutation({ mutationFn: (fn: () => Promise<unknown>) => fn(), onSuccess: refresh });
  const send = useMutation({
    mutationFn: (text: string) => api.sendChat(id ?? null, { text, ...(!id ? { providerId: choice?.providerId ?? initial.providerId, model: choice?.model ?? initial.model, reasoningEffort: effort } : {}) }),
    onSuccess: (result, text) => {
      if (readLocal(key) === text) { setDraft(''); writeLocal(key, ''); }
      qc.setQueryData<{ pages: typeof result[]; pageParams: string[] }>(['chat', result.chat.id], (old) => old?.pages.length ? {
        ...old, pages: [{ ...result, messages: [...new Map([...old.pages[0].messages, ...result.messages].map((m) => [m.id, m])).values()], nextCursor: old.pages[0].nextCursor }, ...old.pages.slice(1)],
      } : { pages: [result], pageParams: [''] });
      void qc.invalidateQueries({ queryKey: ['chats'] });
      stick.current = true;
      if (!id) navigate(`/chat/${result.chat.id}`, { replace: true });
    },
  });
  const submit = () => { if (!draft.trim() || active || chat?.archivedAt || chat?.archiveOperation || send.isPending) return; send.mutate(draft); };
  useEffect(() => { writeLocal(key, draft); }, [key, draft]);
  useEffect(() => {
    if (id && query.error instanceof ApiError && query.error.status === 404) writeLocal('cobot.chat.selected', '');
  }, [id, query.error]);
  useEffect(() => { if (page) for (const run of page.runs) if (!isChatRunning(run.status)) forgetChatRun(run.id); }, [page]);
  useEffect(() => {
    const el = scroll.current;
    if (!el) return;
    const restore = Number(readLocal('cobot.chat.scroll.' + id));
    if (restore > 0) { el.scrollTop = restore; stick.current = false; }
    return () => { writeLocal('cobot.chat.scroll.' + id, String(el.scrollTop)); };
  }, [id, !!page]);
  useEffect(() => {
    if (stick.current && scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight;
  }, [messages.length, active?.status, live]);
  const toBottom = () => { stick.current = true; setBottom(true); scroll.current?.scrollTo({ top: scroll.current.scrollHeight, behavior: 'smooth' }); };
  const error = send.error ?? action.error ?? query.error;
  const modelLoading = engine.isPending || engineModels.isPending || providers.isPending || settings.isPending;
  const modelOptions = !id && !offered.length ? <Button size="sm" variant="ghost" disabled={modelLoading} onClick={() => navigate('/settings/models')}>{t(modelLoading ? 'common.loading' : 'chat.chooseModel')}</Button>
    : !id ? <ModelEffortPicker triggerLabel={choice?.label ?? (initial.model || t('chat.defaultModel'))}
    models={offered.map((m) => ({ id: m.key, label: m.label, group: m.group, disabled: m.unavailable }))}
    model={picked ?? choiceKey(initial.providerId, initial.model)} onModel={setPicked}
    levels={(chosenModel?.reasoningEfforts ?? []).map(({ effort: value }) => ({ value, label: t(`effort.level.${value}`, { defaultValue: value }) }))}
    effort={effort} onEffort={setEffort} isDefault={!picked && !effort} onReset={() => { setPicked(null); setEffort(''); }}
    labels={{ effort: t('effort.title'), reset: t('chat.defaultModel'), chooseModel: t('chat.chooseModel'), back: t('common.back') }} />
    : <span className="truncate px-2 text-xs text-neutral-500" title={t('chat.modelFixed')}>{chat?.model || t('chat.defaultModel')}</span>;

  return <section className="flex min-h-0 flex-1 flex-col" aria-label={t('chat.chat')}>
    <header className="flex min-h-14 items-center gap-2 px-4">
      <ShellToggle place="header" />
      <div className="min-w-0 flex-1">{chat && <h1 className="truncate text-sm font-medium">{chat.title}</h1>}</div>
      {chat && <ChatMenu chat={chat} busy={!!active || pending.length > 0} />}
    </header>
    {id && query.isPending ? <p role="status" className="m-auto text-sm text-neutral-500">{t('common.loading')}</p> : <div className={cx('flex min-h-0 flex-1 flex-col', !id && 'overflow-y-auto')}>
      {id && <div ref={scroll} onScroll={() => { const el = scroll.current!; stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80; setBottom(stick.current); }} className="min-h-0 flex-1 overflow-y-auto px-5">
        <div className="mx-auto w-full max-w-3xl space-y-6 py-6">
          {query.hasNextPage && <Button variant="ghost" size="sm" loading={query.isFetchingNextPage} onClick={() => { const el = scroll.current; const height = el?.scrollHeight ?? 0; void query.fetchNextPage().then(() => requestAnimationFrame(() => { if (el) el.scrollTop += el.scrollHeight - height; })); }}>{t('chat.earlier')}</Button>}
          {messages.map((message) => <div key={message.id} className="space-y-3">
            {message.kind === 'assistant' && runs.has(message.runId) && <ChatProcess run={runs.get(message.runId)!} chatId={id!} />}
            <MessageBubble from={message.kind === 'user' ? 'user' : 'member'} meta={<CopyButton value={message.text} copyLabel={t('chat.copy')} copiedLabel={t('chat.copied')} />}>
              {message.kind === 'user' ? message.text : <Markdown text={message.text} chatId={id} />}
            </MessageBubble>
          </div>)}
          {[...runs.values()].filter((run) => !messages.some((m) => m.runId === run.id && m.kind === 'assistant')).map((run) => <ChatProcess key={run.id} run={run} chatId={id!} />)}
          {active && live[active.id]?.reply && <div className="text-sm leading-7" data-testid="chat-streaming"><Markdown text={live[active.id].reply} chatId={id} /></div>}
          {active?.status === 'unknown' && <div className="rounded-xl bg-sunk p-4"><p className="text-sm">{t('chat.unknown')}</p><Button size="sm" className="mt-3" loading={action.isPending} onClick={() => action.mutate(() => api.chatRunAction(id!, active.id, 'verify'))}>{t('chat.verify')}</Button></div>}
        </div>
      </div>}
      <div className={cx('mx-auto w-full px-5', id ? 'max-w-3xl pb-5 pt-2' : 'my-auto max-w-[40rem] py-12')}>
        {chat && (chat.archivedAt || chat.archiveOperation) && <div className="mb-3 rounded-xl bg-sunk p-3 text-sm"><p role="status">{t(chat.archiveOperation ? 'chatNav.archivePending' : 'chatNav.archivedNotice')}</p>
          <Button className="mt-2" size="sm" disabled={action.isPending} onClick={() => action.mutate(() => api.organizeChat(chat.id, chat.archiveOperation || 'restore', { expectedRevision: chat.revision }))}>{t(chat.archiveOperation ? 'chatNav.verifyArchive' : 'chatNav.restore')}</Button>
        </div>}
        {!id && <ChatWelcome hasDraft={!!draft} />}
        {!bottom && <div className="mb-2 text-center"><Button size="sm" variant="secondary" onClick={toBottom}>{t('chat.bottom')}</Button></div>}
        {error && <p role="alert" className="mb-3 text-sm text-red-600 dark:text-red-400">{apiErrorMessage(t, error)}</p>}
        {pending.map((req) => <ChatRequestCard key={req.id} request={req} busy={action.isPending} onReply={(body) => action.mutate(() => api.replyChatRequest(id!, req.id, { ...body, expectedRevision: req.revision }))} />)}
        <ChatComposer value={draft} onChange={setDraft} onSend={submit} placeholder={t('chat.placeholder')} sendLabel={t('chat.send')} stopLabel={t('chat.stop')} running={!!active && isChatRunning(active.status)} stopping={action.isPending || !!active?.stopRequested}
          canSend={!active && !chat?.archivedAt && !chat?.archiveOperation} sending={send.isPending} onStop={() => active && action.mutate(() => api.chatRunAction(id!, active.id, 'interrupt'))}
          options={modelOptions} />
      </div>
    </div>}
  </section>;
}

type InputQuestion = { id: string; header: string; question: string; isSecret?: boolean; options?: { label: string; description: string }[] };
function ChatRequestCard({ request, busy, onReply }: { request: ChatRequest; busy: boolean; onReply: (body: { optionId?: string; answers?: Record<string, string[]> }) => void }) {
  const { t } = useTranslation();
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const questions = (request.params?.questions ?? []) as InputQuestion[];
  const input = request.method === 'item/tool/requestUserInput';
  return <div className="mb-3 max-h-80 overflow-y-auto rounded-2xl border border-neutral-200 bg-card p-4 dark:border-neutral-700">
    <h2 className="mb-3 text-sm font-medium">{t(input ? 'chat.inputRequired' : 'chat.approvalRequired')}</h2>
    {input ? questions.map((q) => <fieldset key={q.id} className="mb-3 space-y-2"><legend className="text-sm">{q.question}</legend>
      {q.options?.map((option) => <Button key={option.label} size="sm" variant={answers[q.id] === option.label ? 'secondary' : 'ghost'} onClick={() => setAnswers({ ...answers, [q.id]: option.label })}>{option.label}</Button>)}
      <TextInput type={q.isSecret ? 'password' : 'text'} aria-label={q.question} value={answers[q.id] ?? ''} onChange={(event) => setAnswers({ ...answers, [q.id]: event.target.value })} />
    </fieldset>) : <NativeApprovalBody summary={request.summary} fallback={request.reason || request.summary} />}
    <div className="mt-3 flex flex-wrap gap-2">{input ? <Button size="sm" loading={busy} disabled={!questions.length || questions.some((q) => !answers[q.id]?.trim())} onClick={() => onReply({ answers: Object.fromEntries(Object.entries(answers).map(([key, value]) => [key, [value]])) })}>{t('chat.submitAnswers')}</Button>
      : request.options.map((option) => <Button key={option.id} size="sm" disabled={option.disabled || busy} title={option.disabledReasonKey ? t(option.disabledReasonKey) : undefined} onClick={() => onReply({ optionId: option.id })}>{option.messageKey ? t(option.messageKey) : option.text ?? option.id}</Button>)}
    </div>
  </div>;
}
