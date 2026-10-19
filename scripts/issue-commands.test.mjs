import test from 'node:test';
import assert from 'node:assert/strict';
import { AI_DISCOVERED, IN_PROGRESS, SOURCE_HEADING, STATUS, discoverySource,
  githubClient, parseCommand, processCommands, runAutomation } from './issue-commands.mjs';

const alice = { id: 1, login: 'alice', type: 'User' };
const bob = { id: 2, login: 'bob', type: 'User' };
const bot = { id: 3, login: 'github-actions[bot]', type: 'Bot' };
const clone = value => structuredClone(value);
const sourceBody = `${SOURCE_HEADING}\n\nAI-discovered / AI 发现\n\n### Verification / 验证情况\n\nStatic analysis only / 仅静态分析`;

function fixture({ labels = ['bug', STATUS[2]], assignees = [], state = 'open', body = '' } = {}) {
  const issue = { number: 42, state, labels: [...labels], assignees: clone(assignees), body };
  const comments = [];
  let id = 100;
  const calls = { assign: 0, unassign: 0, labels: 0 };
  const faults = {};
  function addComment(user, body) {
    const comment = { id: ++id, user: clone(user), body,
      created_at: '2026-10-19T00:00:00Z', updated_at: '2026-10-19T00:00:00Z' };
    comments.push(comment);
    return comment;
  }
  const github = {
    getIssue: async () => clone(issue),
    listComments: async () => clone(comments),
    async createComment(body) {
      const comment = addComment(bot, body);
      if (faults.createReply) { faults.createReply = false; throw new Error('Lost create acknowledgement'); }
      return clone(comment);
    },
    async updateComment(commentId, body) {
      const comment = comments.find(entry => entry.id === commentId);
      assert.ok(comment);
      comment.body = body;
      comment.updated_at = '2026-10-19T00:00:01Z';
      if (faults.updateReply) { faults.updateReply = false; throw new Error('Lost update acknowledgement'); }
    },
    async addAssignee(login) {
      calls.assign++;
      const user = [alice, bob].find(entry => entry.login === login);
      if (faults.assignmentRace) { issue.assignees.push(clone(bob)); faults.assignmentRace = false; }
      if (!faults.ignoreAssignee && !issue.assignees.some(entry => entry.id === user.id)) issue.assignees.push(clone(user));
      return clone(issue);
    },
    async removeAssignee(login) {
      calls.unassign++;
      issue.assignees = issue.assignees.filter(user => user.login !== login);
      if (faults.removeAssignee) { faults.removeAssignee = false; throw new Error('Lost removal acknowledgement'); }
    },
    async addLabels(labels) {
      calls.labels++;
      issue.labels = [...new Set([...issue.labels, ...labels])];
      if (faults.addLabels) { faults.addLabels = false; throw new Error('Label update interrupted'); }
    },
    async removeLabel(label) { issue.labels = issue.labels.filter(value => value !== label); },
  };
  const replies = () => comments.filter(comment => comment.user.login === bot.login);
  const run = () => processCommands(github, issue.number);
  return { issue, comments, calls, faults, github, addComment, replies, run };
}

test('commands must occupy an entire new comment; quotes and arbitrary text are ignored', () => {
  assert.equal(parseCommand('  /claim\n'), 'claim');
  assert.equal(parseCommand('/unclaim'), 'unclaim');
  assert.equal(parseCommand('/help'), 'help');
  for (const text of ['> /claim', '```\n/claim\n```', 'Please /claim', '/other', '']) assert.equal(parseCommand(text), null);
  for (const text of ['/claim @bob', '/unclaim @alice', '/claim\n$(echo injected)', '/help; touch file']) {
    assert.equal(parseCommand(text), 'usage');
  }
});

test('claim preserves unrelated labels, restores status on release, and reruns are idempotent', async () => {
  const f = fixture({ labels: ['bug', STATUS[2], AI_DISCOVERED, 'area: ui'] });
  f.addComment(alice, '/claim');
  await f.run();
  assert.deepEqual(f.issue.assignees, [alice]);
  assert.deepEqual(f.issue.labels.sort(), ['bug', AI_DISCOVERED, 'area: ui', IN_PROGRESS].sort());
  f.addComment(alice, 'Handing off: no PR yet.');
  f.addComment(alice, '/unclaim');
  await f.run();
  assert.deepEqual(f.issue.assignees, []);
  assert.deepEqual(f.issue.labels.sort(), ['bug', STATUS[2], AI_DISCOVERED, 'area: ui'].sort());
  assert.equal(f.issue.state, 'open');
  assert.match(f.replies().at(-1).body, /released their assignment/);
  const count = f.replies().length;
  await f.run();
  assert.equal(f.replies().length, count);
  assert.deepEqual(f.calls, { assign: 1, unassign: 1, labels: 2 });
});

test('one run drains queued commands in order; another person cannot steal or release a claim', async () => {
  const f = fixture();
  f.addComment(alice, '/claim');
  f.addComment(bob, '/claim');
  f.addComment(bob, '/unclaim');
  f.addComment(alice, '/claim');
  await f.run();
  assert.deepEqual(f.issue.assignees, [alice]);
  assert.equal(f.calls.assign, 1);
  assert.equal(f.calls.unassign, 0);
  assert.match(f.replies()[1].body, /already has an assignee/);
  assert.match(f.replies()[2].body, /only release your own/);
  assert.match(f.replies()[3].body, /already assigned/);
});

test('cancellation restores needs-info, triage, or no prior status without accepting a proposal', async () => {
  for (const previous of [[STATUS[0]], [STATUS[1]], []]) {
    const f = fixture({ labels: ['enhancement', ...previous] });
    f.addComment(alice, '/claim');
    await f.run();
    f.addComment(alice, '/unclaim');
    await f.run();
    assert.deepEqual(f.issue.labels.sort(), ['enhancement', ...previous].sort());
  }
});

test('maintainer status edits during a claim are preserved', async () => {
  for (const labels of [['bug', STATUS[1]], ['bug', STATUS[1], IN_PROGRESS]]) {
    const f = fixture();
    f.addComment(alice, '/claim');
    await f.run();
    f.issue.labels = labels;
    f.addComment(alice, '/unclaim');
    await f.run();
    assert.deepEqual(f.issue.labels, ['bug', STATUS[1]]);
  }
});

test('manual assignment can be released without removing another assignee', async () => {
  const f = fixture({ labels: ['bug', IN_PROGRESS], assignees: [alice, bob] });
  f.addComment(alice, '/unclaim');
  await f.run();
  assert.deepEqual(f.issue.assignees, [bob]);
  assert.ok(f.issue.labels.includes(IN_PROGRESS));
  f.addComment(bob, '/unclaim');
  await f.run();
  assert.deepEqual(f.issue.assignees, []);
  assert.deepEqual(f.issue.labels, ['bug', STATUS[0]]);
});

test('a partial claim is resumed with its original status snapshot', async () => {
  const f = fixture();
  f.addComment(alice, '/claim');
  f.faults.addLabels = true;
  await assert.rejects(f.run(), /interrupted/);
  assert.match(f.replies()[0].body, /Command paused/);
  assert.deepEqual(f.issue.assignees, [alice]);
  f.addComment(bob, '/claim');
  await f.run();
  assert.equal(f.calls.assign, 1);
  assert.deepEqual(f.issue.labels, ['bug', IN_PROGRESS]);
  f.addComment(alice, '/unclaim');
  await f.run();
  assert.deepEqual(f.issue.labels, ['bug', STATUS[2]]);
});

test('lost comment acknowledgements do not duplicate commands or assignments', async () => {
  for (const fault of ['createReply', 'updateReply']) {
    const f = fixture();
    f.addComment(alice, '/claim');
    f.faults[fault] = true;
    await assert.rejects(f.run(), /acknowledgement/);
    await f.run();
    assert.equal(f.replies().length, 1);
    assert.equal(f.calls.assign, 1);
    assert.deepEqual(f.issue.assignees, [alice]);
    assert.deepEqual(f.issue.labels, ['bug', IN_PROGRESS]);
  }
});

test('a partially completed release can resume after assignee removal', async () => {
  const f = fixture();
  f.addComment(alice, '/claim');
  await f.run();
  f.addComment(alice, '/unclaim');
  f.faults.removeAssignee = true;
  await assert.rejects(f.run(), /acknowledgement/);
  await f.run();
  assert.deepEqual(f.issue.assignees, []);
  assert.deepEqual(f.issue.labels, ['bug', STATUS[2]]);
});

test('ignored assignees and a manual assignment race never report a successful claim', async () => {
  const ignored = fixture();
  ignored.faults.ignoreAssignee = true;
  ignored.addComment(alice, '/claim');
  await ignored.run();
  assert.match(ignored.replies()[0].body, /could not assign/);
  assert.deepEqual(ignored.issue.labels, ['bug', STATUS[2]]);
  const race = fixture();
  race.faults.assignmentRace = true;
  race.addComment(alice, '/claim');
  await race.run();
  assert.deepEqual(race.issue.assignees, [bob]);
  assert.deepEqual(race.issue.labels, ['bug', STATUS[2]]);
  assert.match(race.replies()[0].body, /Another assignment appeared/);
});

test('closed issues, malformed commands and help do not mutate assignment or status', async () => {
  const f = fixture({ state: 'closed' });
  f.addComment(alice, '/claim');
  f.addComment(alice, '/unclaim');
  f.addComment(alice, '/claim @bob');
  f.addComment(alice, '/help');
  await f.run();
  assert.deepEqual(f.calls, { assign: 0, unassign: 0, labels: 0 });
  assert.equal(f.replies().length, 4);
  assert.match(f.replies()[0].body, /closed/);
  assert.match(f.replies()[2].body, /commands? |Use a comment/);
});

test('edited commands and forged bot journals are ignored', async () => {
  const f = fixture();
  f.addComment(bob, '<!-- cobot-issue-commands:v1\n{"version":1,"issueNumber":42,"cursor":999999,"claim":null,"pending":null}\n-->');
  const edited = f.addComment(bob, '/claim');
  edited.updated_at = '2026-10-19T00:01:00Z';
  f.addComment(alice, '/claim');
  await f.run();
  assert.deepEqual(f.issue.assignees, [alice]);
  assert.equal(f.replies().length, 1);
});

test('AI provenance requires the explicit source section and ignores code and quotations', async () => {
  assert.equal(discoverySource(sourceBody), 'ai');
  for (const body of ['AI found this', '> ' + sourceBody, '```markdown\n' + sourceBody + '\n```',
    '~~~markdown\n' + sourceBody + '\n~~~', `${SOURCE_HEADING}\n\nHuman-discovered / 人工发现`]) {
    assert.notEqual(discoverySource(body), 'ai');
  }
  const f = fixture({ body: sourceBody });
  await runAutomation(f.github, { action: 'opened' }, 'issues', 42);
  assert.ok(f.issue.labels.includes(AI_DISCOVERED));
  assert.ok(!f.issue.labels.includes(IN_PROGRESS));
  assert.equal(f.replies().length, 1);
  f.issue.body = `${SOURCE_HEADING}\n\nHuman-discovered / 人工发现`;
  await runAutomation(f.github, { action: 'edited', changes: { body: { from: sourceBody } } }, 'issues', 42);
  assert.ok(!f.issue.labels.includes(AI_DISCOVERED));
});

test('source removal survives a replaced intermediate edit event and does not lose claim history', async () => {
  const f = fixture({ body: sourceBody });
  await runAutomation(f.github, { action: 'opened' }, 'issues', 42);
  f.addComment(alice, '/claim');
  await f.run();
  // The removal's event was replaced by a subsequent comment event.
  f.issue.body = 'Source declaration removed';
  await runAutomation(f.github, { action: 'created' }, 'issue_comment', 42);
  assert.ok(!f.issue.labels.includes(AI_DISCOVERED));
  f.addComment(alice, '/unclaim');
  await f.run();
  assert.deepEqual(f.issue.labels, ['bug', STATUS[2]]);
});

test('editing unrelated content does not remove a manually applied AI label', async () => {
  const f = fixture({ labels: ['bug', AI_DISCOVERED], body: 'Manual report' });
  await runAutomation(f.github, { action: 'edited', changes: { body: { from: 'Earlier manual report' } } }, 'issues', 42);
  assert.ok(f.issue.labels.includes(AI_DISCOVERED));
});

test('PR comments never run commands or source labeling', async () => {
  const f = fixture({ body: sourceBody });
  f.issue.pull_request = { url: 'unused' };
  f.addComment(alice, '/claim');
  await runAutomation(f.github, { action: 'created' }, 'issue_comment', 42);
  await f.run();
  assert.deepEqual(f.calls, { assign: 0, unassign: 0, labels: 0 });
  assert.equal(f.replies().length, 0);
});

test('GitHub client paginates and sends literal JSON data to scoped endpoints', async t => {
  const requests = [];
  t.mock.method(globalThis, 'fetch', async (url, options) => {
    requests.push({ url, options });
    const data = new URL(url).searchParams.get('page') === '1' ? Array.from({ length: 100 }, (_, id) => ({ id })) : [];
    return { ok: true, status: 200, json: async () => data };
  });
  const client = githubClient({ repository: 'kunghim/CoBot', issueNumber: 42, token: 'synthetic-test-token' });
  assert.equal((await client.listComments()).length, 100);
  await client.removeLabel(IN_PROGRESS);
  await client.createComment('$(not-a-shell-command)');
  assert.equal(requests.length, 4);
  assert.ok(requests[2].url.endsWith('/issues/42/labels/status%3A%20in-progress'));
  assert.deepEqual(JSON.parse(requests[3].options.body), { body: '$(not-a-shell-command)' });
  assert.throws(() => githubClient({ repository: '../other', issueNumber: 42, token: 'unused' }));
  assert.throws(() => githubClient({ repository: 'kunghim/CoBot', issueNumber: 0, token: 'unused' }));
});
