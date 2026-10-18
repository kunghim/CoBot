// Developer-only orchestration; the installed product requires no Node.
import { spawn, execFileSync } from 'node:child_process';
import { createWriteStream, existsSync, readFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve, join } from 'node:path';
import { homedir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { setTimeout as sleep } from 'node:timers/promises';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const index = process.argv.indexOf('--data-root');
if (index >= 0 && !process.argv[index + 1]) throw new Error('--data-root requires a path');
const profile = resolve(index >= 0 ? process.argv[index + 1] : join(homedir(), '.cobot-dev'));
const instanceFile = join(profile, 'runtime/instance.json');
const secretFile = join(profile, 'runtime/credentials/instance-admin.secret');
const npm = process.platform === 'win32' ? 'npm.exe' : 'npm';
const go = process.platform === 'win32' ? 'go.exe' : 'go';
const origin = 'http://127.0.0.1:5173';
let ownedCore, vite, log, endpoint, adminSecret;
let stopping = false;

async function internal(path) {
  const response = await fetch(endpoint + path, {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Instance-Secret': adminSecret },
    body: JSON.stringify({ requestId: crypto.randomUUID() }), signal: AbortSignal.timeout(4000),
  });
  if (!response.ok) throw new Error(`core request failed (${response.status})`);
  return response;
}
async function discover() {
  if (!existsSync(instanceFile) || !existsSync(secretFile)) return false;
  const info = JSON.parse(readFileSync(instanceFile, 'utf8'));
  const url = new URL(info.endpoint);
  if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) || info.profile !== 'development') throw new Error('Existing instance is not a local development core');
  endpoint = info.endpoint;
  adminSecret = readFileSync(secretFile, 'utf8').trim();
  try { await internal('/internal/v1/session/link'); return true; }
  catch (err) {
    if (String(err).includes('core request failed')) throw err;
    return false;
  }
}
async function cleanup() {
  if (stopping) return;
  stopping = true;
  vite?.kill();
  if (ownedCore && ownedCore.exitCode === null && existsSync(instanceFile) && JSON.parse(readFileSync(instanceFile, 'utf8')).pid === ownedCore.pid) {
    try {
      await internal('/internal/v1/service/stop');
      for (let i = 0; i < 120 && ownedCore.exitCode === null; i++) await sleep(100);
      if (ownedCore.exitCode === null) throw new Error('owned core did not finish graceful shutdown; inspect developer log');
    } catch (err) { console.error(String(err)); process.exitCode = 1; }
  }
  log?.end();
}

try {
  if (!existsSync(join(root, 'frontend/node_modules'))) execFileSync(npm, ['ci', '--prefix', 'frontend'], { cwd: root, stdio: 'inherit', windowsHide: true });
  if (!await discover()) {
    execFileSync(npm, ['run', 'build', '--prefix', 'frontend'], { cwd: root, stdio: 'inherit', windowsHide: true });
    mkdirSync(join(root, '.tmp/bin'), { recursive: true });
    const binary = join(root, '.tmp/bin', `cobot-dev-${process.pid}${process.platform === 'win32' ? '.exe' : ''}`);
    execFileSync(go, ['build', '-o', binary, './cmd/cobot'], { cwd: root, stdio: 'inherit', windowsHide: true });
    log = createWriteStream(join(root, '.tmp', `dev-core-${process.pid}.log`));
    ownedCore = spawn(binary, ['serve', '--profile', 'development', '--data-root', profile, '--listen', '127.0.0.1:0', '--dev-origin', origin, '--no-open'], { cwd: root, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
    let partial = '';
    ownedCore.stdout.on('data', (chunk) => {
      partial += chunk.toString();
      let pos;
      while ((pos = partial.indexOf('\n')) >= 0) {
        const line = partial.slice(0, pos); partial = partial.slice(pos + 1);
        if (!line.includes('#cobot-bootstrap=')) log.write(line + '\n');
      }
    });
    ownedCore.stderr.pipe(log, { end: false });
    for (let i = 0; i < 150; i++) {
      if (ownedCore.exitCode !== null) throw new Error('core exited before startup; inspect developer log');
      if (await discover()) break;
      if (i === 149) throw new Error('core did not become ready');
      await sleep(100);
    }
  }
  const probe = await fetch(endpoint + '/api/v1/status', { headers: { Origin: origin }, signal: AbortSignal.timeout(4000) });
  if (!probe.ok) throw new Error('Existing core does not allow Vite; restart with --dev-origin http://127.0.0.1:5173');
  process.on('SIGINT', () => { void cleanup(); });
  process.on('SIGTERM', () => { void cleanup(); });
  console.log(`Development Web: ${origin} (${ownedCore ? 'owns core' : 'reuses existing core'})`);
  vite = spawn(process.execPath, [join(root, 'frontend/node_modules/vite/bin/vite.js')], {
    cwd: join(root, 'frontend'), stdio: 'inherit', windowsHide: true,
    env: { ...process.env, COBOT_API_ENDPOINT: endpoint, COBOT_ADMIN_SECRET: adminSecret },
  });
  const exit = await new Promise((done) => { vite.on('exit', (code) => done(code)); vite.on('error', () => done(1)); });
  if (!stopping && exit) process.exitCode = exit;
} catch (err) { console.error(String(err)); process.exitCode = 1; }
finally { await cleanup(); }
