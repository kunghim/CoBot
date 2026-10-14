// No model turns, login calls or credential reads. Pin an explicitly supplied binary
// and MXC; never retry with a legacy sandbox or full-access permissions.
import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { Client } from './client.mjs';

assert.equal(process.platform, 'win32', 'Windows-only probe');
const binary = process.env.CODEX_BIN;
assert.ok(binary && path.isAbsolute(binary), 'Set CODEX_BIN to the verified real executable, not a shim');
const version = spawnSync(binary, ['--version'], { encoding: 'utf8', windowsHide: true });
assert.equal(version.status, 0, 'Cannot identify executable');
const root = path.resolve('.tmp/app-server-spike/windows-coexistence-' + Date.now());
const nativeHome = path.resolve(process.env.CODEX_HOME || path.join(os.homedir(), '.codex'));
const pwsh = process.env.SPIKE_PWSH || 'pwsh.exe';
const clients = [];
const report = { startedAt: new Date().toISOString(), version: version.stdout.trim(), cases: [], passed: false };
await fs.mkdir(root, { recursive: true });
const quote = value => "'" + value.replaceAll("'", "''") + "'";
const record = (name, facts) => { report.cases.push({ name, ...facts }); console.log(JSON.stringify({ name, ...facts })); };
const digest = async file => {
  try { return crypto.createHash('sha256').update(await fs.readFile(file)).digest('hex'); }
  catch (e) { if (e.code === 'ENOENT') return null; throw e; }
};
async function nativeState() {
  // Metadata only for auth; do not read authentication values.
  const auth = await fs.stat(path.join(nativeHome, 'auth.json')).catch(e => { if (e.code !== 'ENOENT') throw e; });
  const acl = spawnSync(pwsh, ['-NoProfile', '-Command', `(Get-Acl -LiteralPath ${quote(path.join(nativeHome, '.sandbox-bin'))}).Sddl`], { encoding: 'utf8', windowsHide: true });
  assert.equal(acl.status, 0, 'Cannot inspect native sandbox directory ACL');
  return {
    config: await digest(path.join(nativeHome, 'config.toml')),
    setupMarker: await digest(path.join(nativeHome, '.sandbox', 'setup_marker.json')),
    setupError: await digest(path.join(nativeHome, '.sandbox', 'setup_error.json')),
    authMetadata: auth ? { size: auth.size, mtimeMs: auth.mtimeMs } : null,
    sandboxBinAcl: acl.stdout.trim(),
  };
}
async function open(home, cwd = root) {
  const env = { ...process.env, RUST_LOG: 'off', SPIKE_PREFER_MXC: '0' };
  const args = ['-c', 'windows.sandbox="mxc"', '-c', 'features.prefer_mxc=true'];
  if (home) {
    Object.assign(env, { CODEX_HOME: home, CODEX_SQLITE_HOME: home });
    for (const key of ['OPENAI_API_KEY', 'CODEX_API_KEY', 'CODEX_ACCESS_TOKEN', 'ACCESS_TOKEN', 'OPENAI_ACCESS_TOKEN']) delete env[key];
    args.push('-c', 'cli_auth_credentials_store="ephemeral"', '-c', `sqlite_home=${JSON.stringify(home.replaceAll('\\', '/'))}`);
  }
  const c = new Client({ cwd, binary, env, args }); clients.push(c);
  c.on('request', m => c.send({ id: m.id, error: { code: -32601, message: 'Outside this bounded command-only probe' } }));
  await c.initialize();
  const cfg = (await c.rpc('config/read', { includeLayers: false })).config;
  assert.equal(cfg.windows.sandbox, 'mxc', 'Explicit MXC required; stop without fallback');
  return c;
}
async function close(c) {
  if (c.child.exitCode !== null || c.child.signalCode !== null) return;
  let timer;
  const exited = new Promise(resolve => c.child.once('exit', () => resolve(true)));
  c.child.stdin.end();
  const clean = await Promise.race([exited, new Promise(resolve => { timer = setTimeout(() => resolve(false), 8000); })]);
  clearTimeout(timer);
  if (!clean) {
    c.child.kill();
    let killTimer;
    await Promise.race([exited, new Promise(resolve => { killTimer = setTimeout(resolve, 3000); })]);
    clearTimeout(killTimer);
    throw new Error('App-server did not exit cleanly');
  }
}
async function exec(c, cwd, script, sandboxPolicy) {
  const r = await c.rpc('command/exec', { command: [pwsh, '-NoProfile', '-Command', script], cwd, sandboxPolicy, timeoutMs: 10000 }, 20000);
  assert.equal(r.exitCode, 0, 'Bounded command failed');
  return r.stdout;
}
async function nativeCheck(c, name) {
  const text = await exec(c, root, "Write-Output 'NATIVE_OK'", { type: 'readOnly' });
  assert.ok(text.includes('NATIVE_OK'));
  const forbidden = path.join(root, name + '-readonly-write.txt');
  const blocked = await exec(c, root, `try { [IO.File]::WriteAllText(${quote(forbidden)},'UNEXPECTED'); 'UNEXPECTED' } catch { 'WRITE_BLOCKED' }`, { type: 'readOnly' });
  assert.ok(blocked.includes('WRITE_BLOCKED'));
  await assert.rejects(fs.access(forbidden), { code: 'ENOENT' });
  record(name, { commandCompleted: true, readOnlyWriteBlocked: true, sandbox: 'mxc', policy: 'readOnly' });
}
const before = await nativeState();
try {
  const native = await open(); await nativeCheck(native, 'native-before');
  const envs = [];
  for (const name of ['production', 'development']) {
    const home = path.join(root, name + '-codex'), cwd = path.join(root, name + '-work');
    await fs.mkdir(home); await fs.mkdir(cwd);
    // workspaceWrite includes the server's startup cwd. Pin it to this environment's
    // work directory, not the common parent used to collect evidence.
    const c = await open(home, cwd);
    envs.push({ name, home, cwd, c });
  }
  // Both isolated processes remain live while the native instance executes.
  const work = envs.map(async e => {
    const marker = path.join(e.cwd, 'marker.txt'), value = 'COBOT_' + e.name.toUpperCase();
    const policy = { type: 'workspaceWrite', writableRoots: [e.cwd], networkAccess: false };
    const text = await exec(e.c, e.cwd, `[IO.File]::WriteAllText(${quote(marker)},${quote(value)}); [IO.File]::ReadAllText(${quote(marker)})`, policy);
    assert.ok(text.includes(value)); assert.equal(await fs.readFile(marker, 'utf8'), value);
    const forbidden = path.join(root, e.name + '-outside.txt');
    const blocked = await exec(e.c, e.cwd, `try { [IO.File]::WriteAllText(${quote(forbidden)},'UNEXPECTED'); 'UNEXPECTED' } catch { 'WRITE_BLOCKED' }`, policy);
    assert.ok(blocked.includes('WRITE_BLOCKED'));
    await assert.rejects(fs.access(forbidden), { code: 'ENOENT' });
    record(e.name + '-execution', { markerMatched: true, outsideWriteBlocked: true, sandbox: 'mxc' });
  });
  const results = await Promise.allSettled([...work, nativeCheck(native, 'native-during')]);
  for (const result of results) if (result.status === 'rejected') throw result.reason;
  for (const e of envs) {
    await close(e.c);
    const restarted = await open(e.home, e.cwd);
    const text = await exec(restarted, e.cwd, `[IO.File]::ReadAllText(${quote(path.join(e.cwd, 'marker.txt'))})`, { type: 'readOnly' });
    assert.ok(text.includes('COBOT_' + e.name.toUpperCase()));
    await close(restarted);
    record(e.name + '-process-restart', { previousFileReadable: true, cleanExit: true });
    await assert.rejects(fs.access(path.join(e.home, 'auth.json')), { code: 'ENOENT' });
    await assert.rejects(fs.access(path.join(e.home, '.sandbox-bin')), { code: 'ENOENT' });
  }
  await nativeCheck(native, 'native-after'); await close(native);
  const restartedNative = await open(); await nativeCheck(restartedNative, 'native-app-server-restart'); await close(restartedNative);
  report.passed = true;
} catch (e) {
  // Do not persist raw RPC errors, stderr or configuration contents.
  record('failed', { errorClass: e.name, errorCode: e.code ?? null });
  process.exitCode = 1;
} finally {
  for (const c of clients) {
    try { await close(c); } catch { report.passed = false; process.exitCode = 1; record('process-cleanup-failed', { cleanExit: false }); }
  }
  const after = await nativeState();
  const unchanged = JSON.stringify(before) === JSON.stringify(after);
  const { authMetadata: beforeAuth, ...beforeOther } = before;
  const { authMetadata: afterAuth, ...afterOther } = after;
  record('native-state', { configSetupAndAclUnchanged: JSON.stringify(beforeOther) === JSON.stringify(afterOther), authMetadataUnchanged: JSON.stringify(beforeAuth) === JSON.stringify(afterAuth), credentialValuesRead: false });
  if (!unchanged) { report.passed = false; process.exitCode = 1; }
  report.finishedAt = new Date().toISOString();
  await fs.writeFile(path.join(root, 'report.json'), JSON.stringify(report, null, 2));
  console.log('REPORT ' + path.join(root, 'report.json'));
}
