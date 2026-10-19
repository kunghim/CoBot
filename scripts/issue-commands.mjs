// Issue metadata only: never execute comment text or check out contributor code.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

export const STATUS = ['status: needs-triage', 'status: needs-info', 'status: accepted'];
export const IN_PROGRESS = 'status: in-progress';
export const AI_DISCOVERED = 'ai-discovered';
export const SOURCE_HEADING = '### Discovery source / 发现来源';
const BOT = 'github-actions[bot]';
const MARKER = 'cobot-issue-commands:v1';
const HELP = 'Use a comment containing only `/claim`, `/unclaim`, or `/help`. '
  + '`/claim` assigns you and marks work in progress; `/unclaim` releases only your assignment and restores the previous status. '
  + 'Claiming does not approve a feature or merge a PR.\n\n'
  + '请单独评论 `/claim`、`/unclaim` 或 `/help`。认领自动分配给自己并标记正在处理；取消只移除自己的认领并恢复之前状态。认领不代表需求获批，PR 仍由维护者决定。';

const names = issue => issue.labels.map(label => typeof label === 'string' ? label : label.name);
const identity = user => Number.isSafeInteger(user?.id) && user.id > 0
  && typeof user.login === 'string' && /^[a-z\d][a-z\d-]*(?:\[bot\])?$/i.test(user.login);
const trusted = comment => comment.user?.login === BOT && comment.user.type === 'Bot';

export function parseCommand(body) {
  const text = (body ?? '').trim();
  if (/^\/(claim|unclaim|help)$/.test(text)) return text.slice(1);
  return /^\/(claim|unclaim|help)\b/.test(text) ? 'usage' : null;
}

export function discoverySource(body) {
  let fence = null;
  const lines = (body ?? '').split(/\r?\n/).filter(line => {
    const delimiter = line.trim().match(/^(`{3,}|~{3,})/);
    if (delimiter) {
      const character = delimiter[1][0];
      if (!fence) fence = character;
      else if (fence === character) fence = null;
      return false;
    }
    return !fence;
  });
  const start = lines.findIndex(line => line.trim() === SOURCE_HEADING);
  if (start < 0) return null;
  const value = lines.slice(start + 1).find(line => line.trim());
  if (value?.trim() === 'AI-discovered / AI 发现') return 'ai';
  return ['Human-discovered / 人工发现', 'Unsure / 不确定'].includes(value?.trim()) ? 'other' : null;
}

function encodeReply(message, state) {
  return `${message}\n\n<!-- ${MARKER}\n${JSON.stringify(state)}\n-->`;
}

function decodeState(comment, issueNumber) {
  if (!trusted(comment)) return null;
  const match = comment.body?.match(/<!-- cobot-issue-commands:v1\n([^\n]+)\n-->/);
  if (!match) return null;
  try {
    const state = JSON.parse(match[1]);
    if (state.version !== 1 || state.issueNumber !== issueNumber
        || !Number.isSafeInteger(state.cursor) || state.cursor < 0) return null;
    if (state.claim && (!identity(state.claim.owner) || !Array.isArray(state.claim.priorStatus)
        || state.claim.priorStatus.some(label => !STATUS.includes(label)))) return null;
    if (state.pending && (!['claim', 'unclaim'].includes(state.pending.command)
        || !identity(state.pending.actor) || !Number.isSafeInteger(state.pending.sourceId)
        || state.pending.sourceId <= state.cursor || !Array.isArray(state.pending.priorStatus)
        || state.pending.priorStatus.some(label => !STATUS.includes(label)))) return null;
    return state;
  } catch { return null; }
}

function findRecord(comments, issueNumber) {
  return [...comments].sort((a, b) => b.id - a.id)
    .map(comment => ({ comment, state: decodeState(comment, issueNumber) })).find(entry => entry.state);
}

const initialState = issueNumber => ({ version: 1, issueNumber, cursor: 0, claim: null, pending: null, aiSourceManaged: false });

async function syncDiscoverySource(github, issue, event, issueNumber) {
  const record = findRecord(await github.listComments(), issueNumber);
  const state = record?.state ?? initialState(issueNumber);
  const source = discoverySource(issue.body);
  if (source === 'ai') {
    // Journal ownership first so a later edit or retry can remove this label,
    // even if Actions replaces an intermediate queued edit event.
    if (!state.aiSourceManaged) {
      const next = { ...state, aiSourceManaged: true };
      const message = record ? record.comment.body.split(`\n\n<!-- ${MARKER}`)[0]
        : 'Discovery source is declared as AI; this does not establish verification or acceptance. / 来源声明为 AI 发现，不代表已经验证或接受。';
      if (record) await github.updateComment(record.comment.id, encodeReply(message, next));
      else await github.createComment(encodeReply(message, next));
    }
    if (!names(issue).includes(AI_DISCOVERED)) await github.addLabels([AI_DISCOVERED]);
  } else if (source === 'other' || state.aiSourceManaged || discoverySource(event.changes?.body?.from) === 'ai') {
    if (names(issue).includes(AI_DISCOVERED)) await github.removeLabel(AI_DISCOVERED);
    if (record && state.aiSourceManaged) {
      const message = !state.cursor && !state.pending
        ? 'The AI discovery declaration was removed or corrected; its label has been updated. / AI 发现声明已移除或修正，标签已同步更新。'
        : record.comment.body.split(`\n\n<!-- ${MARKER}`)[0];
      await github.updateComment(record.comment.id, encodeReply(message, { ...state, aiSourceManaged: false }));
    }
  }
}

async function applyClaim(github, plan, previousClaim) {
  let issue = await github.getIssue();
  if (issue.state !== 'open') return { claim: previousClaim, message: 'This issue is closed; no assignment changed. / Issue 已关闭，未修改负责人。' };
  if (issue.assignees.some(user => user.id !== plan.actor.id)) {
    return { claim: previousClaim, message: 'This issue already has an assignee; no assignment changed. / 已有人认领，未覆盖现有负责人。' };
  }
  if (!issue.assignees.some(user => user.id === plan.actor.id)) {
    // GitHub may silently ignore an ineligible assignee; verify the response.
    issue = await github.addAssignee(plan.actor.login);
    if (!issue.assignees.some(user => user.id === plan.actor.id)) {
      return { claim: previousClaim, message: 'GitHub could not assign this account. A maintainer can check its eligibility. / GitHub 未能分配此账号，请维护者核对账号是否可分配。' };
    }
    // A manual assignment may race the request. Preserve the other assignee.
    if (issue.assignees.some(user => user.id !== plan.actor.id)) {
      await github.removeAssignee(plan.actor.login);
      return { claim: previousClaim, message: 'Another assignment appeared; your claim was not applied. / 已出现其他负责人，本次未认领。' };
    }
  }
  await github.addLabels([IN_PROGRESS]);
  for (const label of STATUS) await github.removeLabel(label);
  return {
    claim: { owner: plan.actor, priorStatus: plan.priorStatus },
    message: `Claimed by @${plan.actor.login}; status is in progress. / 已由 @${plan.actor.login} 认领，状态为正在处理。`,
  };
}

async function applyUnclaim(github, plan, previousClaim) {
  let issue = await github.getIssue();
  if (issue.state !== 'open') return { claim: previousClaim, message: 'This issue is closed; no assignment changed. / Issue 已关闭，未修改负责人。' };
  const actor = issue.assignees.find(user => user.id === plan.actor.id);
  if (actor) await github.removeAssignee(actor.login);
  issue = await github.getIssue();
  if (!issue.assignees.length && names(issue).includes(IN_PROGRESS)) {
    // Preserve a status a maintainer added while work was in progress.
    if (!names(issue).some(label => STATUS.includes(label))) await github.addLabels(plan.priorStatus);
    await github.removeLabel(IN_PROGRESS);
  }
  const claim = previousClaim && issue.assignees.some(user => user.id === previousClaim.owner.id) ? previousClaim : null;
  return {
    claim,
    message: `@${plan.actor.login} released their assignment. The issue remains open; other assignees and maintainer status changes are preserved. / @${plan.actor.login} 已取消自己的认领。Issue 保持开放，其他负责人及维护者调整的状态不变。`,
  };
}

export async function processCommands(github, issueNumber) {
  // Actions keeps only one pending run per concurrency group. Drain every
  // unprocessed comment, so replacing a queued run does not drop its command.
  const comments = (await github.listComments()).sort((a, b) => a.id - b.id);
  const record = findRecord(comments, issueNumber);
  let state = record?.state ?? initialState(issueNumber);
  async function finish(commentId) {
    const plan = state.pending;
    try {
      const outcome = plan.command === 'claim'
        ? await applyClaim(github, plan, state.claim)
        : await applyUnclaim(github, plan, state.claim);
      const next = { ...state, cursor: plan.sourceId, claim: outcome.claim, pending: null };
      await github.updateComment(commentId, encodeReply(outcome.message, next));
      state = next;
    } catch (error) {
      try {
        await github.updateComment(commentId, encodeReply(
          'Command paused after an API error; some metadata may already have changed. A maintainer can rerun the workflow to finish. / API 操作失败，命令已暂停；部分信息可能已修改，维护者可重跑工作流继续完成。', state));
      } catch { /* Keep the previous operation journal if even the reply fails. */ }
      throw error;
    }
  }
  // A pending bot reply is an operation journal. A rerun completes partial
  // assignment/label updates without taking a new snapshot or claiming twice.
  if (state.pending) await finish(record.comment.id);
  for (const comment of comments) {
    if (comment.id <= state.cursor || trusted(comment) || !identity(comment.user)
        || comment.created_at !== comment.updated_at) continue;
    const command = parseCommand(comment.body);
    if (!command) continue;
    const issue = await github.getIssue();
    if (issue.pull_request) return;
    const actor = { id: comment.user.id, login: comment.user.login };
    let message;
    if (command === 'help' || command === 'usage') message = HELP;
    else if (issue.state !== 'open') message = 'This issue is closed; no assignment changed. / Issue 已关闭，未修改负责人。';
    else if (command === 'claim' && issue.assignees.length) {
      message = issue.assignees.some(user => user.id === actor.id)
        ? 'You are already assigned; nothing changed. / 你已是负责人，未重复修改。'
        : 'This issue already has an assignee; no assignment changed. / 已有人认领，未覆盖现有负责人。';
    } else if (command === 'unclaim' && !issue.assignees.some(user => user.id === actor.id)) {
      message = 'You can only release your own assignment. / 只能取消自己的认领，未修改其他负责人。';
    }
    if (message) {
      const next = { ...state, cursor: comment.id };
      await github.createComment(encodeReply(message, next));
      state = next;
      continue;
    }
    const priorStatus = command === 'claim'
      ? names(issue).filter(label => STATUS.includes(label))
      : state.claim?.owner.id === actor.id ? state.claim.priorStatus : [STATUS[0]];
    const pending = { command, actor, priorStatus, sourceId: comment.id };
    const next = { ...state, pending };
    const reply = await github.createComment(encodeReply(
      'Processing this command. If the workflow fails, a maintainer can rerun it to finish safely. / 正在处理；工作流若失败，维护者可重跑以继续完成操作。', next));
    state = next;
    await finish(reply.id);
  }
}

export async function runAutomation(github, event, eventName, issueNumber) {
  if (!['issues', 'issue_comment', 'workflow_dispatch'].includes(eventName)) throw new Error('Unsupported event');
  const issue = await github.getIssue();
  if (issue.pull_request) return;
  // A queued edit run can be replaced by a comment run: always reconcile the
  // current declaration rather than relying on delivery of every edit event.
  await syncDiscoverySource(github, issue, event, issueNumber);
  await processCommands(github, issueNumber);
}

export function githubClient({ repository, issueNumber, token, apiUrl = 'https://api.github.com' }) {
  if (!/^[a-z\d][a-z\d_-]*\/[a-z\d][a-z\d_.-]*$/i.test(repository) || !Number.isSafeInteger(issueNumber) || issueNumber < 1) {
    throw new Error('Invalid repository or issue number');
  }
  const base = `${apiUrl.replace(/\/$/, '')}/repos/${repository}`;
  async function request(method, path, body) {
    const response = await fetch(base + path, {
      method,
      headers: { Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json',
        'X-GitHub-Api-Version': '2022-11-28', 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(30_000),
    });
    if (response.status === 404 && method === 'DELETE' && path.includes('/labels/')) return;
    if (!response.ok) throw new Error(`GitHub API ${method} failed (${response.status}); rerun after resolving the API error`);
    return response.status === 204 ? undefined : response.json();
  }
  const issuePath = `/issues/${issueNumber}`;
  return {
    getIssue: () => request('GET', issuePath),
    async listComments() {
      const comments = [];
      for (let page = 1; ; page++) {
        const batch = await request('GET', `${issuePath}/comments?per_page=100&page=${page}`);
        comments.push(...batch);
        if (batch.length < 100) return comments;
      }
    },
    createComment: body => request('POST', `${issuePath}/comments`, { body }),
    updateComment: (id, body) => request('PATCH', `/issues/comments/${id}`, { body }),
    addAssignee: login => request('POST', `${issuePath}/assignees`, { assignees: [login] }),
    removeAssignee: login => request('DELETE', `${issuePath}/assignees`, { assignees: [login] }),
    addLabels: labels => labels.length ? request('POST', `${issuePath}/labels`, { labels }) : Promise.resolve(),
    removeLabel: label => request('DELETE', `${issuePath}/labels/${encodeURIComponent(label)}`),
  };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, 'utf8'));
    const requestedNumber = event.issue?.number ?? event.inputs?.issue_number;
    // Keep dispatch concurrency keys identical to comment-event keys.
    if (!/^[1-9]\d*$/.test(String(requestedNumber))) throw new Error('Use a positive issue number without leading zeros');
    const issueNumber = Number(requestedNumber);
    const token = process.env.GITHUB_TOKEN;
    if (!token) throw new Error('GITHUB_TOKEN is required');
    const github = githubClient({ repository: process.env.GITHUB_REPOSITORY, issueNumber, token,
      apiUrl: process.env.GITHUB_API_URL });
    await runAutomation(github, event, process.env.GITHUB_EVENT_NAME, issueNumber);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
