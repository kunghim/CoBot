import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { Client, messageText, nativeItems } from './client.mjs';

const root = path.resolve(process.env.SPIKE_OUTPUT || '.tmp/app-server-spike/run-' + Date.now());
fs.mkdirSync(root, { recursive: true });
const report = { startedAt: new Date().toISOString(), node: process.version, cases: [], requests: [] };
const clients = [];
const save = () => fs.writeFileSync(path.join(root, 'report.json'), JSON.stringify(report, null, 2));
const log = (...a) => console.log(...a);
const newClient = () => { const c = new Client({ cwd: root }); clients.push(c); c.on('request', m => { report.requests.push({ method: m.method, threadId: m.params?.threadId, turnId: m.params?.turnId }); c.reply(m.id, { decision: 'decline' }); }); return c; };
const config = { 'mcp_servers.node_repl.enabled': false, 'mcp_servers.rea.enabled': false };
const dev = 'You are a bounded CoBot integration-test bot. Do not delegate. Only use the test working directory. Do not inspect parent directories, credentials, configuration, environment variables or unrelated files. Follow the exact small test task; no extra tests or exploration.';
async function caseRun(name, fn) {
  const t = Date.now(); log('START', name);
  try { const details = await fn(); report.cases.push({ name, status: 'passed', ms: Date.now() - t, ...details }); log('PASS', name, JSON.stringify(details)); }
  catch (e) { report.cases.push({ name, status: 'failed', ms: Date.now() - t, error: e.message }); log('FAIL', name, e.message); }
  save();
}
let a, b, c;
try {
  c = newClient(); const init = await c.initialize(); report.platform = { os: init.platformOs, family: init.platformFamily };
  const acc = await c.rpc('account/read', { refreshToken: false }); report.accountType = acc.account?.type;
  await caseRun('basic-and-parallel', async () => {
    const dirs = ['alpha', 'beta'].map(n => path.join(root, n));
    dirs.forEach((d, i) => { fs.mkdirSync(d); fs.writeFileSync(path.join(d, 'seed.txt'), i === 0 ? 'ALPHA_73' : 'BETA_91'); });
    [a, b] = await Promise.all(dirs.map((cwd, i) => c.rpc('thread/start', { cwd, sandbox: 'workspace-write', approvalPolicy: 'on-request', approvalsReviewer: 'user', config, developerInstructions: dev + ` Your role is ${i === 0 ? 'alpha' : 'beta'}. Remember your role in this conversation.` })));
    fs.writeFileSync(path.join(root, 'thread-ids.json'), JSON.stringify({ alpha: a.thread.id, beta: b.thread.id }));
    const turns = await Promise.all([a, b].map(t => c.turn(t.thread.id, 'Use native tools to read seed.txt, create result.txt containing exactly the seed value, and run one shell command to read result.txt. Do not request elevated permissions. Reply with your role and the seed value.')));
    fs.writeFileSync(path.join(root, 'native-results.json'), JSON.stringify(turns.map((r, i) => ({ threadId: [a, b][i].thread.id, status: r.turn.status, answer: messageText(r.events), items: nativeItems(r.events) })), null, 2));
    const details = turns.map((r, i) => {
      const expected = i === 0 ? 'ALPHA_73' : 'BETA_91', text = messageText(r.events), items = nativeItems(r.events);
      assert.equal(r.turn.status, 'completed', JSON.stringify(r.turn.error));
      assert.equal(fs.readFileSync(path.join(dirs[i], 'result.txt'), 'utf8').trim(), expected);
      assert.ok(text.includes(expected)); assert.ok(text.includes(i === 0 ? 'alpha' : 'beta'));
      assert.ok(r.events.some(m => m.method === 'item/agentMessage/delta'));
      assert.ok(items.some(i => i.type === 'commandExecution' && i.status === 'completed'));
      return { threadId: [a, b][i].thread.id, model: [a, b][i].model, provider: [a, b][i].modelProvider, status: r.turn.status, answer: text, itemTypes: items.map(i => i.type), eventMethods: [...new Set(r.events.map(m => m.method))] };
    });
    fs.writeFileSync(path.join(root, 'thread-ids.json'), JSON.stringify({ alpha: a.thread.id, beta: b.thread.id }));
    return { threads: details };
  });
  if (a) await caseRun('normal-resume', async () => {
    await c.close(); c = newClient(); await c.initialize();
    const r = await c.rpc('thread/resume', { threadId: a.thread.id, config, approvalPolicy: 'on-request', approvalsReviewer: 'user', sandbox: 'workspace-write' });
    assert.equal(r.thread.id, a.thread.id);
    const t = await c.turn(a.thread.id, 'Without using tools, state your remembered role and the exact seed value from the previous turn.');
    assert.equal(t.turn.status, 'completed'); const text = messageText(t.events);
    assert.ok(text.includes('alpha'));
    if (report.cases.find(x => x.name === 'basic-and-parallel')?.status === 'passed') assert.ok(text.includes('ALPHA_73'));
    return { threadId: r.thread.id, answer: text, status: t.turn.status };
  });
  await caseRun('mcp-per-thread-identity', async () => {
    const script = fileURLToPath(new URL('./mock-mcp.mjs', import.meta.url)), receipts = path.join(root, 'mcp-receipts.jsonl');
    const ts = await Promise.all(['alpha', 'beta'].map(bot => c.rpc('thread/start', { cwd: path.join(root, bot), sandbox: 'workspace-write', approvalPolicy: 'on-request', approvalsReviewer: 'user', developerInstructions: dev + ` Your role is ${bot}.`, config: { ...config, 'mcp_servers.cobot_spike.command': process.execPath, 'mcp_servers.cobot_spike.args': [script, bot, receipts], 'mcp_servers.cobot_spike.required': true, 'mcp_servers.cobot_spike.default_tools_approval_mode': 'auto' } })));
    const rs = await Promise.all(ts.map((t, i) => c.turn(t.thread.id, `Call the cobot_spike send_message MCP tool exactly once with target="${i === 0 ? 'beta' : 'alpha'}" and text="MCP_${i === 0 ? 'ALPHA' : 'BETA'}_CHECK". Do not use shell or write files. Then report the receipt returned by the tool.`)));
    for (const r of rs) assert.equal(r.turn.status, 'completed', JSON.stringify(r.turn.error));
    const lines = fs.readFileSync(receipts, 'utf8').trim().split('\n').map(s => JSON.parse(s));
    assert.equal(lines.length, 2);
    for (const bot of ['alpha', 'beta']) assert.ok(lines.some(l => l.caller === bot && l.target === (bot === 'alpha' ? 'beta' : 'alpha') && l.text === `MCP_${bot.toUpperCase()}_CHECK`));
    return { receipts: lines, threads: ts.map((t, i) => ({ threadId: t.thread.id, answer: messageText(rs[i].events), eventMethods: [...new Set(rs[i].events.map(m => m.method))] })) };
  });
} finally {
  for (const client of clients) await client.close(); report.finishedAt = new Date().toISOString(); save();
  log('REPORT', root, 'failed cases:', report.cases.filter(c => c.status === 'failed').length);
  if (report.cases.some(c => c.status === 'failed')) process.exitCode = 1;
}
