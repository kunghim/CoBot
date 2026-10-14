// Direct command execution removes model/tool-selection uncertainty.
// Both variants retain workspaceWrite + networkAccess:false.
import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert/strict';
import { Client } from './client.mjs';
const root = path.resolve('.tmp/app-server-spike/shell-diagnostic-' + Date.now());
fs.mkdirSync(root, { recursive: true });
const report = { startedAt: new Date().toISOString(), cases: [] };
for (const mode of ['configured', 'prefer-mxc']) {
  const c = new Client({ cwd: root, args: ['-c', `features.prefer_mxc=${mode === 'prefer-mxc'}`] });
  const entry = { mode, policy: { type: 'workspaceWrite', writableRoots: [root], networkAccess: false }, status: 'failed' };
  try {
    await c.initialize();
    const cfg = await c.rpc('config/read', { includeLayers: false });
    entry.config = { legacySandbox: cfg.config?.windows?.sandbox, preferMxc: cfg.config?.features?.prefer_mxc ?? false };
    const marker = mode + '-marker.txt';
    const result = await c.rpc('command/exec', { command: [process.env.SPIKE_PWSH || 'pwsh.exe', '-NoProfile', '-Command', `[IO.File]::WriteAllText('${marker}','SHELL_CHECK'); [IO.File]::ReadAllText('${marker}')`], cwd: root, sandboxPolicy: entry.policy, timeoutMs: 10000 });
    entry.result = result;
    assert.equal(result.exitCode, 0); assert.ok(result.stdout.includes('SHELL_CHECK'));
    assert.equal(fs.readFileSync(path.join(root, marker), 'utf8'), 'SHELL_CHECK');
    const outside = root + '-outside'; fs.mkdirSync(outside, { recursive: true });
    const outsideMarker = path.join(outside, mode + '.txt');
    const blocked = await c.rpc('command/exec', { command: [process.env.SPIKE_PWSH || 'pwsh.exe', '-NoProfile', '-Command', `try { [IO.File]::WriteAllText('${outsideMarker}','UNEXPECTED_WRITE'); 'UNEXPECTED_WRITE' } catch { 'WRITE_BLOCKED' }`], cwd: root, sandboxPolicy: entry.policy, timeoutMs: 10000 });
    entry.outsideWrite = blocked;
    assert.equal(fs.existsSync(outsideMarker), false, 'Sandbox allowed write outside configured workspace');
    assert.ok(blocked.stdout.includes('WRITE_BLOCKED'));
    entry.status = 'passed';
  } catch (e) { entry.error = e.message; }
  finally { await c.close(); }
  report.cases.push(entry); console.log(JSON.stringify(entry));
}
report.finishedAt = new Date().toISOString();
fs.writeFileSync(path.join(root, 'report.json'), JSON.stringify(report, null, 2));
console.log('REPORT', root);
if (report.cases.every(x => x.status === 'failed')) process.exitCode = 1;
