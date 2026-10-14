import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { Client, nativeItems, messageText } from './client.mjs';
const root = path.resolve(process.env.SPIKE_OUTPUT || '.tmp/app-server-spike/approval-' + Date.now());
fs.mkdirSync(root, { recursive: true });
const c = new Client({ cwd: root });
const report = { startedAt: new Date().toISOString(), preferMxc: process.env.SPIKE_PREFER_MXC === '1', cases: [] };
let current;
c.on('request', async m => {
  const p = m.params || {}, command = p.command || '';
  const fileItem = c.events.findLast(e => e.method === 'item/started' && e.params?.item?.id === p.itemId)?.params?.item;
  const paths = (fileItem?.changes || []).map(x => x.path);
  const expected = current?.marker;
  const bounded = current && p.threadId === current.threadId && (
    (m.method === 'item/commandExecution/requestApproval' && /^"[^"\r\n]*\\pwsh\.exe" -(?:NoProfile -)?Command "\[IO\.File\]::WriteAllText\('approval-marker\.txt','APPROVAL_ONCE'\)"$/i.test(command) && p.cwd && path.resolve(p.cwd) === current.cwd) ||
    (m.method === 'item/fileChange/requestApproval' && paths.length === 1 && path.resolve(paths[0]) === expected && (!p.grantRoot || path.resolve(p.grantRoot) === path.dirname(expected)))
  );
  const record = { id: m.id, method: m.method, threadId: p.threadId, turnId: p.turnId, itemId: p.itemId, command, changes: paths, bounded: !!bounded, existedBeforeDecision: expected ? fs.existsSync(expected) : null };
  current?.requests.push(record);
  if (!bounded) { c.reply(m.id, m.method === 'item/permissions/requestApproval' ? { permissions: {} } : { decision: 'decline' }); return; }
  await new Promise(r => setTimeout(r, 500));
  record.existedWhileWaiting = fs.existsSync(expected);
  record.decision = current.decision;
  c.reply(m.id, { decision: record.decision });
});
try {
  await c.initialize();
  for (const kind of ['command', 'file']) for (const decision of ['accept', 'decline']) {
    const name = `${kind}-${decision}`, cwd = path.join(root, name), target = path.join(root, name + '-target');
    fs.mkdirSync(cwd); fs.mkdirSync(target);
    const marker = path.join(kind === 'command' ? cwd : target, 'approval-marker.txt');
    const t = await c.rpc('thread/start', { cwd, sandbox: 'workspace-write', approvalPolicy: 'untrusted', approvalsReviewer: 'user', config: { 'mcp_servers.node_repl.enabled': false, 'mcp_servers.rea.enabled': false }, developerInstructions: 'This is a bounded native-approval integration test. Do not delegate or inspect unrelated files. Run only the explicitly requested single operation. Do not retry, choose another tool, or bypass permissions if it is denied. Your only authorized write target is the marker in the user message.' });
    current = { name, cwd, marker, decision, threadId: t.thread.id, requests: [] };
    console.log('START', name);
    try {
      const prompt = kind === 'command'
        ? 'Use the native shell tool to execute exactly this PowerShell statement in your cwd: [IO.File]::WriteAllText(\'approval-marker.txt\',\'APPROVAL_ONCE\'). This should require native approval. Do not use a file-edit tool. After one attempt, report the result; if denied do not retry.'
        : `Use the native apply_patch/file-edit tool once to create ${marker} with content APPROVAL_ONCE. This is outside your writable cwd but inside an isolated test directory. Request native file approval if needed; do not use a shell or request_permissions tool, and do not retry if denied.`;
      const r = await c.turn(t.thread.id, prompt);
      Object.assign(current, { turnStatus: r.turn.status, answer: messageText(r.events), items: nativeItems(r.events), eventMethods: [...new Set(r.events.map(m => m.method))] });
      const handled = current.requests.filter(x => x.bounded);
      assert.ok(handled.length > 0, 'No matching native approval was observed');
      assert.equal(handled.length, 1, 'Expected one decision');
      assert.ok(handled.every(x => !x.existedBeforeDecision && !x.existedWhileWaiting), 'Action occurred before approval');
      if (decision === 'accept') assert.equal(fs.readFileSync(marker, 'utf8').trim(), 'APPROVAL_ONCE');
      else assert.equal(fs.existsSync(marker), false);
      report.cases.push({ ...current, status: 'passed' });
      console.log('PASS', name);
    } catch (e) { report.cases.push({ ...current, status: 'failed', error: e.message }); console.log('FAIL', name, e.message); }
    fs.writeFileSync(path.join(root, 'report.json'), JSON.stringify(report, null, 2));
    current = null;
  }
} finally {
  await c.close(); report.finishedAt = new Date().toISOString(); fs.writeFileSync(path.join(root, 'report.json'), JSON.stringify(report, null, 2));
  console.log('REPORT', root); if (report.cases.some(x => x.status === 'failed')) process.exitCode = 1;
}
