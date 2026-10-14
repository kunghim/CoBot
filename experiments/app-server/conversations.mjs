import path from 'node:path';
import fs from 'node:fs';
import assert from 'node:assert/strict';
import { Client, messageText } from './client.mjs';
const root = path.resolve('.tmp/app-server-spike/conversation-' + Date.now());
fs.mkdirSync(root, { recursive: true });
let c = new Client({ cwd: root });
const report = { startedAt: new Date().toISOString(), cases: [] };
const config = { 'mcp_servers.node_repl.enabled': false, 'mcp_servers.rea.enabled': false };
try {
  await c.initialize();
  const starts = await Promise.all(['alpha', 'beta'].map(role => {
    const cwd = path.join(root, role); fs.mkdirSync(cwd);
    return c.rpc('thread/start', { cwd, sandbox: 'read-only', approvalPolicy: 'on-request', approvalsReviewer: 'user', config, developerInstructions: `You are ${role}. This is a conversation-routing test. Do not use tools, delegate, or inspect files. Follow the short user request.` });
  }));
  const ids = starts.map(t => t.thread.id); assert.notEqual(ids[0], ids[1]);
  const rs = await Promise.all(starts.map((t, i) => c.turn(t.thread.id, `Remember the conversation marker ${i === 0 ? 'ALPHA_73' : 'BETA_91'}. Reply only with your role and this marker.`)));
  rs.forEach((r, i) => {
    const text = messageText(r.events); assert.equal(r.turn.status, 'completed'); assert.ok(text.includes(i === 0 ? 'ALPHA_73' : 'BETA_91')); assert.ok(!text.includes(i === 0 ? 'BETA_91' : 'ALPHA_73'));
    assert.equal(starts[i].cwd, path.join(root, i === 0 ? 'alpha' : 'beta'));
    assert.ok(r.events.some(m => m.method === 'item/agentMessage/delta'));
  });
  const intervals = rs.map(r => ({ started: r.turn.startedAt, ended: r.turn.completedAt }));
  assert.ok(Math.max(...intervals.map(x => x.started)) < Math.min(...intervals.map(x => x.ended)), 'Turns did not overlap');
  report.cases.push({ name: 'parallel-conversation-routing', status: 'passed', threads: starts.map((t, i) => ({ threadId: t.thread.id, role: i === 0 ? 'alpha' : 'beta', cwd: t.cwd, model: t.model, provider: t.modelProvider, answer: messageText(rs[i].events), interval: intervals[i] })) });
  console.log('PASS parallel-conversation-routing');
  await c.close(); c = new Client({ cwd: root }); await c.initialize();
  await c.rpc('thread/resume', { threadId: ids[0], config, sandbox: 'read-only', approvalPolicy: 'on-request', approvalsReviewer: 'user' });
  const r = await c.turn(ids[0], 'Reply only with your role and the conversation marker from my previous message.');
  const answer = messageText(r.events); assert.equal(r.turn.status, 'completed'); assert.ok(answer.includes('alpha') && answer.includes('ALPHA_73') && !answer.includes('BETA_91'));
  report.cases.push({ name: 'normal-resume', status: 'passed', threadId: ids[0], answer }); console.log('PASS normal-resume');
} catch (e) { report.cases.push({ status: 'failed', error: e.message }); console.error('FAIL', e.message); process.exitCode = 1; }
finally { await c.close(); report.finishedAt = new Date().toISOString(); fs.writeFileSync(path.join(root, 'report.json'), JSON.stringify(report, null, 2)); console.log('REPORT', root); }
