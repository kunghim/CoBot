import { beforeEach, describe, expect, it } from 'vitest';
import { applyLiveSignal, forgetLiveRuns, liveRunsSnapshot, mergeSteps, resetLiveStore } from '../../api/live';
import type { Message, Run, RunStep, RunWithSteps } from '../../api/types';
import { buildTimeline } from './ConversationView';
import { messageText } from './delivery';
import { isExchange, taskCardOf } from './ExchangeLine';
import { linkTarget } from './links';
import { describeStep, displayCommand, durationPhrase, runPhase, isPartOfReply, turnBlocks, visibleSteps, workNeedsAttention, workSummary } from './steps';

const step = (overrides: Partial<RunStep>): RunStep => ({
  id: 's', runId: 'r1', teamId: 't', botId: 'b', seq: 1, kind: 'command', status: 'completed',
  detail: {}, startedAt: '2026-10-17T10:00:00Z', updatedAt: '2026-10-17T10:00:01Z', ...overrides,
});

describe('linkTarget', () => {
  it('sends paths to the core and web links to the browser', () => {
    expect(linkTarget('https://example.com')).toEqual({ kind: 'web' });
    expect(linkTarget('/E:/cobot-trial-ws/index.html')).toEqual({ kind: 'local', path: '/E:/cobot-trial-ws/index.html' });
    expect(linkTarget('E:\\ws\\a.txt')).toEqual({ kind: 'local', path: 'E:\\ws\\a.txt' });
    expect(linkTarget('src/app.ts:12')).toEqual({ kind: 'local', path: 'src/app.ts:12' });
    expect(linkTarget('file:///E:/ws/a.txt').kind).toBe('local');
  });
  it('blocks every other scheme', () => {
    for (const href of ['javascript:alert(1)', 'data:text/html,x', 'vbscript:x', '//evil.example/x', '#top', '']) {
      expect(linkTarget(href)).toEqual({ kind: 'blocked' });
    }
  });
});

describe('describeStep', () => {
  const nameOf = (id: string) => ({ qa: '测试工程师' })[id] ?? id;
  it('does not describe a declined command as executed', () => {
    expect(describeStep(step({ status: 'declined' }), nameOf).key).toBe('conversation.step.commandDeclined');
    expect(describeStep(step({ status: 'completed' }), nameOf).key).toBe('conversation.step.command');
  });
  it('describes collaboration tools in plain words', () => {
    const phrase = describeStep(step({ kind: 'tool', detail: { server: 'cobot', tool: 'send_message', arguments: { targetBotId: 'qa', text: 'hi' } } }), nameOf);
    expect(phrase).toEqual(expect.objectContaining({ key: 'conversation.step.cobot.send_message', params: expect.objectContaining({ name: '测试工程师' }) }));
    const fromString = describeStep(step({ kind: 'tool', detail: { server: 'cobot', tool: 'start_requirement', arguments: '{"title":"做页面"}' } }), nameOf);
    expect(fromString).toEqual(expect.objectContaining({ key: 'conversation.step.cobot.start_requirement', params: expect.objectContaining({ title: '做页面' }) }));
  });
  it('describes a member working with its sub-agents', () => {
    expect(describeStep(step({ kind: 'agent', detail: { tool: 'spawnAgent', agents: 2, prompt: 'x' } }), nameOf)).toEqual({ key: 'conversation.step.agent.spawnAgent', params: { count: 2 } });
    expect(describeStep(step({ kind: 'agent', detail: { activity: 'completed', agent: '/root/tester' } }), nameOf)).toEqual({ key: 'conversation.step.agent.activity.completed', params: { agent: '/root/tester' } });
    expect(describeStep(step({ kind: 'agent', detail: { tool: 'newAgentTool', agents: 1 } }), nameOf).key).toBe('conversation.step.agent.other');
    expect(describeStep(step({ kind: 'agent', detail: { activity: 'paused', agent: '/root/tester' } }), nameOf).key).toBe('conversation.step.agent.other');
  });
  it('tells the handoffs of a card apart', () => {
    const tool = (name: string, args: Record<string, unknown>) => describeStep(step({ kind: 'tool', detail: { server: 'cobot', tool: name, arguments: args } }), nameOf).key;
    expect(tool('handoff', { cardId: 't', outcome: 'done', summary: 's' })).toBe('conversation.step.cobot.handoff_done');
    expect(tool('handoff', { cardId: 't', outcome: 'back', mode: 'rerun', summary: 'x' })).toBe('conversation.step.cobot.handoff_back');
    expect(tool('handoff', { cardId: 't', outcome: 'escalate', summary: 'x' })).toBe('conversation.step.cobot.handoff_escalate');
    expect(tool('handoff', { cardId: 't', outcome: 'resume', summary: 'x' })).toBe('conversation.step.cobot.handoff_resume');
    expect(tool('view_tasks', {})).toBe('conversation.step.cobot.view_tasks');
  });
  it('names every recipient of a broadcast note', () => {
    const phrase = describeStep(step({ kind: 'tool', detail: { server: 'cobot', tool: 'send_message', arguments: { targetBotIds: ['qa', 'fe'], text: 'x' } } }), nameOf);
    expect(phrase.key).toBe('conversation.step.cobot.send_message');
    expect(phrase.params?.name).toBe('测试工程师、fe');
  });
  it('explains known engine errors in plain words', () => {
    expect(describeStep(step({ kind: 'notice', detail: { level: 'error', code: 'serverOverloaded', message: 'Selected model is at capacity.' } }), nameOf)).toEqual({ key: 'conversation.engineError.serverOverloaded' });
    expect(describeStep(step({ kind: 'notice', detail: { level: 'error', code: 'other', message: 'boom' } }), nameOf)).toEqual({ key: 'conversation.step.error', params: { message: 'boom' } });
  });
  it('names the root cause a gateway refused a turn for', () => {
    expect(describeStep(step({ kind: 'notice', detail: { level: 'error', code: 'httpConnectionFailed', failureCode: 'quota', message: '余额不足' } }), nameOf)).toEqual({ key: 'conversation.providerError.quota' });
    expect(describeStep(step({ kind: 'notice', detail: { level: 'error', code: 'httpConnectionFailed', failureCode: 'auth_invalid', message: '密钥无效' } }), nameOf)).toEqual({ key: 'conversation.providerError.auth_invalid' });
    // A cause the interface cannot explain falls back to the engine's own
    // wording rather than to a key nobody translated.
    expect(describeStep(step({ kind: 'notice', detail: { level: 'error', code: 'httpConnectionFailed', failureCode: 'something_new', message: 'boom' } }), nameOf)).toEqual({ key: 'conversation.engineError.httpConnectionFailed' });
  });
  it('describes other tools, retries and changes', () => {
    expect(describeStep(step({ kind: 'tool', detail: { server: 'fs', tool: 'read' } }), nameOf)).toEqual({ key: 'conversation.step.tool', params: { tool: 'fs.read' } });
    expect(describeStep(step({ kind: 'retry', status: 'in_progress', detail: { attempt: 3 } }), nameOf)).toEqual({ key: 'conversation.step.retrying', params: { attempt: 3 } });
    expect(describeStep(step({ kind: 'file_change', detail: { changes: [{}, {}] } }), nameOf).params).toEqual({ count: 2 });
  });
});

describe('runPhase', () => {
  it('reports what the turn is doing now', () => {
    const run: Pick<Run, 'status'> = { status: 'running' };
    expect(runPhase({ status: 'starting' }, [], undefined).key).toBe('conversation.phase.starting');
    expect(runPhase(run, [step({ kind: 'retry', status: 'in_progress', detail: { attempt: 2 } })], undefined)).toEqual({ key: 'conversation.phase.reconnecting', params: { attempt: 2 } });
    expect(runPhase(run, [step({ status: 'in_progress' })], undefined).key).toBe('conversation.phase.command');
    expect(runPhase(run, [step({})], { steps: {}, reply: '写', reasoning: {}, output: {}, commentary: {} }).key).toBe('conversation.phase.replying');
    expect(runPhase({ status: 'waiting_approval' }, [step({ status: 'in_progress' })], undefined).key).toBe('conversation.phase.waitingApproval');
    expect(runPhase(run, [], undefined).key).toBe('conversation.phase.thinking');
    expect(runPhase(run, [], undefined, 20).key).toBe('conversation.phase.slow');
    expect(runPhase(run, [step({})], undefined, 20).key).toBe('conversation.phase.thinking');
  });
  it('shows commands without the shell wrapper', () => {
    expect(displayCommand(String.raw`"D:\\Developer\\PowerShell\\7\\pwsh.exe" -Command 'git status --short'`)).toBe('git status --short');
    expect(displayCommand(String.raw`"C:\\pwsh.exe" -Command Get-ChildItem`)).toBe('Get-ChildItem');
    expect(displayCommand(`powershell.exe -NoProfile -Command 'echo ''hi'''`)).toBe(`echo 'hi'`);
    expect(displayCommand(`/bin/bash -lc 'ls -la'`)).toBe('ls -la');
    expect(displayCommand('cmd.exe /c dir')).toBe('dir');
    expect(displayCommand('npm test')).toBe('npm test');
  });
  it('hides empty thoughts and an ongoing reconnect', () => {
    expect(visibleSteps([step({ kind: 'retry', status: 'in_progress' })], undefined)).toHaveLength(0);
    expect(visibleSteps([step({ kind: 'retry', status: 'completed' })], undefined)).toHaveLength(1);
    const steps = [step({ id: 'a', kind: 'reasoning', detail: { text: '' } }), step({ id: 'b', kind: 'reasoning', detail: { text: '先看代码' } })];
    expect(visibleSteps(steps, undefined).map((s) => s.id)).toEqual(['b']);
    expect(visibleSteps(steps, { steps: {}, reply: '', reasoning: { a: '正在想' }, output: {}, commentary: {} }).map((s) => s.id)).toEqual(['a', 'b']);
  });
  it('formats durations', () => {
    expect(durationPhrase(8)).toEqual({ key: 'conversation.duration.seconds', params: { s: 8 } });
    expect(durationPhrase(83)).toEqual({ key: 'conversation.duration.minutes', params: { m: 1, s: 23 } });
  });
});

describe('live store', () => {
  beforeEach(() => resetLiveStore());
  it('streams working remarks into their step, never into the reply', () => {
    applyLiveSignal({ type: 'delta', teamId: 't', botId: 'b', runId: 'r1', itemId: 'm1', kind: 'commentary', text: '我先看' });
    applyLiveSignal({ type: 'delta', teamId: 't', botId: 'b', runId: 'r1', itemId: 'm1', kind: 'commentary', text: '看目录' });
    const live = liveRunsSnapshot().r1;
    expect(live.reply).toBe('');
    expect(live.commentary.m1).toBe('我先看看目录');
    const remark = step({ id: 'm1', kind: 'message', detail: {} });
    expect(visibleSteps([remark], live).map((s) => s.id)).toEqual(['m1']);
    expect(visibleSteps([remark], undefined)).toEqual([]);
  });
  it('accumulates deltas and keeps the newer form of a step', () => {
    applyLiveSignal({ type: 'delta', teamId: 't', botId: 'b', runId: 'r1', kind: 'message', text: '你' });
    applyLiveSignal({ type: 'delta', teamId: 't', botId: 'b', runId: 'r1', kind: 'message', text: '好' });
    applyLiveSignal({ type: 'delta', teamId: 't', botId: 'b', runId: 'r1', itemId: 'c', kind: 'output', text: 'ok' });
    const newer = step({ id: 'c', status: 'completed', updatedAt: '2026-10-17T10:00:05Z' });
    applyLiveSignal({ type: 'step', teamId: 't', botId: 'b', runId: 'r1', step: newer });
    const live = liveRunsSnapshot().r1;
    expect(live.reply).toBe('你好');
    expect(live.output.c).toBe('ok');
    expect(live.steps.c.status).toBe('completed');
    forgetLiveRuns((runId) => runId !== 'r1');
    expect(liveRunsSnapshot().r1).toBeUndefined();
    const stored = [step({ id: 'c', status: 'in_progress', updatedAt: '2026-10-17T10:00:02Z' }), step({ id: 'd', seq: 2 })];
    const merged = mergeSteps(stored, { steps: { c: newer }, reply: '', reasoning: {}, output: {}, commentary: {} });
    expect(merged.map((s) => [s.id, s.status])).toEqual([['c', 'completed'], ['d', 'completed']]);
    const stale = mergeSteps([step({ id: 'c', status: 'completed', updatedAt: '2026-10-17T10:00:09Z' })], { steps: { c: step({ id: 'c', status: 'in_progress' }) }, reply: '', reasoning: {}, output: {}, commentary: {} });
    expect(stale[0].status).toBe('completed');
  });
});

describe('buildTimeline', () => {
  const message = (id: string, at: string, kind: Message['kind'] = 'user'): Message => ({ id, kind, createdAt: at, blocks: [] } as unknown as Message);
  const run = (id: string, at: string): RunWithSteps => ({ id, teamId: 't', botId: 'b', status: 'completed', startedAt: at, steps: [] } as unknown as RunWithSteps);
  it('places a turn after the message that woke it and before its reply', () => {
    const timeline = buildTimeline(
      [message('u1', '2026-10-17T10:00:00Z'), message('a1', '2026-10-17T10:00:30Z', 'assistant')],
      [run('r1', '2026-10-17T10:00:01Z')],
      null,
    );
    expect(timeline.map((entry) => (entry.kind === 'run' ? entry.run.id : entry.message.id))).toEqual(['u1', 'r1', 'a1']);
  });
  it('shows the active run even before the stored page has it', () => {
    const active = { id: 'r2', teamId: 't', botId: 'b', status: 'running', startedAt: '2026-10-17T10:01:00Z' } as unknown as Run;
    const timeline = buildTimeline([message('u1', '2026-10-17T10:00:59Z')], [], active);
    expect(timeline.at(-1)).toEqual(expect.objectContaining({ kind: 'run', run: expect.objectContaining({ id: 'r2', steps: [] }) }));
  });
  it('merges one message sent to several members into one line', () => {
    const sent = (id: string, to: string, at: string) =>
      ({ ...message(id, at, 'collaboration'), botId: to, sourceBotId: 'b', blocks: [{ type: 'text', text: '开工' }] }) as Message;
    const timeline = buildTimeline([sent('m1', 'fe', '2026-10-17T10:00:00Z'), sent('m2', 'qa', '2026-10-17T10:00:01Z'), sent('m3', 'fe', '2026-10-17T10:05:00Z')], [], null, 'b');
    expect(timeline.map((entry) => (entry.kind === 'message' ? entry.recipients : null))).toEqual([['fe', 'qa'], ['fe']]);
  });
});

describe('exchange lines', () => {
  const base = { id: 'm', teamId: 't', createdAt: '2026-10-17T10:00:00Z' };
  it('treats member messages, task assignments and review notices as exchanges', () => {
    expect(isExchange({ ...base, botId: 'fe', kind: 'collaboration', sourceBotId: 'lead', blocks: [] } as Message)).toBe(true);
    const assignment = { ...base, botId: 'fe', kind: 'system', blocks: [{ type: 'text', text: '你被指派了任务…' }, { type: 'task', text: JSON.stringify({ taskId: 't1', title: '做页面' }) }] } as Message;
    expect(isExchange(assignment)).toBe(true);
    expect(isExchange({ ...base, botId: 'lead', kind: 'system', sourceBotId: 'fe', blocks: [{ type: 'text', text: '提交验收' }] } as Message)).toBe(true);
    expect(isExchange({ ...base, botId: 'lead', kind: 'system', blocks: [{ type: 'text', text: '这一轮被中断' }] } as Message)).toBe(false);
  });
});

describe('task assignments', () => {
  const blocks = [
    { type: 'text', text: '你被指派了任务：登录页' },
    { type: 'task', text: JSON.stringify({ taskId: 'task-1', title: '登录页', acceptanceCriteria: '能登录', ownerId: 'fe' }) },
  ];
  it('reads the card and keeps its JSON out of the text', () => {
    expect(taskCardOf(blocks)).toEqual(expect.objectContaining({ taskId: 'task-1', title: '登录页' }));
    expect(messageText({ blocks } as never)).toBe('你被指派了任务：登录页');
    expect(taskCardOf([{ type: 'task', text: '{broken' }])).toBeNull();
  });
  it('reads an assignment written before the card block existed', () => {
    const legacy = taskCardOf([{ type: 'text', text: '你被指派了任务：登录页\n任务 ID：task-9\n详细要求：做表单\n完成标准：能登录\n且能退出' }]);
    expect(legacy).toEqual({ title: '登录页', taskId: 'task-9' });
    expect(taskCardOf([{ type: 'text', text: '普通系统消息' }])).toBeNull();
  });
});

describe('a turn read in the order it happened', () => {
  const said = (id: string, text: string) => step({ id, kind: 'message', detail: { text } });
  it('keeps what was said and the work between the sayings in order, work grouped', () => {
    const steps = [said('m1', '我先看看'), step({ id: 'c1' }), step({ id: 'c2' }), said('m2', '看完了'), step({ id: 'f1', kind: 'file_change' }), said('m3', '改好了')];
    expect(turnBlocks(steps).map((block) => (block.kind === 'text' ? block.step.id : block.steps.map((s) => s.id).join('+')))).toEqual(['m1', 'c1+c2', 'm2', 'f1', 'm3']);
  });
  it('says each stretch of work in a line', () => {
    expect(workSummary([step({ id: 'a' }), step({ id: 'b' }), step({ id: 'c', kind: 'file_change' })])).toEqual([
      { key: 'conversation.process.group.command', params: { count: 2 } },
      { key: 'conversation.process.group.file_change', params: { count: 1 } },
    ]);
    expect(workSummary([step({ kind: 'diff' })])).toEqual([{ key: 'conversation.process.group.other', params: { count: 1 } }]);
  });
  it('leaves open the work that went wrong', () => {
    expect(workNeedsAttention([step({ status: 'completed' })])).toBe(false);
    expect(workNeedsAttention([step({ status: 'failed' })])).toBe(true);
    expect(workNeedsAttention([step({ kind: 'notice' })])).toBe(true);
  });
  it('does not say the reply twice', () => {
    const steps = [said('m1', '我先看看'), step({ id: 'c1' }), said('m2', '看完了，结果是 42')];
    expect(visibleSteps(steps, undefined, '看完了，结果是 42').map((s) => s.id)).toEqual(['m1', 'c1']);
    // Nothing to compare with while the turn runs.
    expect(visibleSteps(steps, undefined).map((s) => s.id)).toEqual(['m1', 'c1', 'm2']);
  });
  it('hides only what the reply is made of, not a short word it happens to contain', () => {
    expect(isPartOfReply('好', '好')).toBe(true);
    expect(isPartOfReply('第一点', '第一点\n\n第二点')).toBe(true);
    expect(isPartOfReply('第二点', '第一点\n\n第二点')).toBe(true);
    expect(isPartOfReply('第二点', '第一点\n\n第二点\n\n第三点')).toBe(true);
    expect(isPartOfReply('好', '好的，已经改好了')).toBe(false);
    const steps = [{ ...step({ id: 'm1', kind: 'message', detail: { text: '好' } }) }, step({ id: 'c1' })];
    expect(visibleSteps(steps, undefined, '好的，已经改好了').map((s) => s.id)).toEqual(['m1', 'c1']);
  });
});
