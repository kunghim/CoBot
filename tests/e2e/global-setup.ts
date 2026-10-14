import { spawn, execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, rmSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { setTimeout as sleep } from 'node:timers/promises';
import { request as pwRequest } from '@playwright/test';

// Builds the real core + fake codex host, starts a core on a throwaway data
// root, exchanges a bootstrap secret, and hands the session to the specs.
const root = resolve(import.meta.dirname, '..', '..');
const runId = `e2e-${Date.now()}`;
const profileRoot = join(root, '.tmp', 'test-runs', runId, 'profile');
const teamDir = join(root, '.tmp', 'test-runs', runId, 'team');
const binDir = join(root, '.tmp', 'bin');
const isWindows = process.platform === 'win32';
const exe = (name: string) => join(binDir, isWindows ? `${name}.exe` : name);
const port = process.env.COBOT_E2E_PORT ?? '18777';
const baseURL = `http://127.0.0.1:${port}`;

function build(): void {
  mkdirSync(binDir, { recursive: true });
  mkdirSync(teamDir, { recursive: true });
  execFileSync('go', ['build', '-o', exe('cobot'), './cmd/cobot'], { cwd: root, stdio: 'inherit' });
  execFileSync('go', ['build', '-o', exe('fake-codex'), './tests/e2e/host'], { cwd: root, stdio: 'inherit' });
  execFileSync('npm', ['run', 'build', '--prefix', 'frontend'], { cwd: root, stdio: 'inherit' });
}

async function waitJSON(url: string, attempts = 100): Promise<any> {
  for (let i = 0; i < attempts; i++) {
    try {
      const resp = await fetch(url);
      if (resp.ok) return await resp.json();
    } catch {
      /* not up yet */
    }
    await sleep(200);
  }
  throw new Error(`timeout waiting for ${url}`);
}

export default async function globalSetup(): Promise<void> {
  if (process.env.COBOT_E2E_SKIP_SETUP === '1') return;
  build();
  rmSync(profileRoot, { recursive: true, force: true });
  mkdirSync(profileRoot, { recursive: true });

  const child = spawn(exe('cobot'), ['serve', '--data-root', profileRoot, '--listen', `127.0.0.1:${port}`], {
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let output = '';
  child.stdout.on('data', (chunk) => (output += String(chunk)));
  child.stderr.on('data', (chunk) => (output += String(chunk)));

  // Wait for the instance document, then mint a bootstrap through the
  // same-user runtime credential (exactly what the dev script does).
  const instanceFile = join(profileRoot, 'runtime', 'instance.json');
  const secretFile = join(profileRoot, 'runtime', 'credentials', 'instance-admin.secret');
  for (let i = 0; i < 100 && !existsSync(instanceFile); i++) await sleep(100);
  const instance = JSON.parse(readFileSync(instanceFile, 'utf-8'));
  const adminSecret = readFileSync(secretFile, 'utf-8').trim();
  const linkResp = await fetch(`${instance.endpoint}/internal/v1/session/link`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Instance-Secret': adminSecret },
    body: JSON.stringify({ requestId: `e2e-${Date.now()}` }),
  });
  const link = (await linkResp.json()) as { data: { bootstrapSecret: string } };

  await waitJSON(`${baseURL}/api/v1/status`);

  // Redeem a startup secret for a session and point the engine at the fake
  // codex host so the full send→run→reply pipeline executes against a
  // scripted engine rather than a real model account.
  const mintResp = await fetch(`${instance.endpoint}/internal/v1/session/link`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Instance-Secret': adminSecret },
    body: JSON.stringify({ requestId: `e2e-mint-${Date.now()}` }),
  });
  const mint = (await mintResp.json()) as { data: { bootstrapSecret: string } };
  const api = await pwRequest.newContext({ baseURL: instance.endpoint });
  const boot = await api.post('/api/v1/session/bootstrap', {
    data: { requestId: `e2e-boot-${Date.now()}`, secret: mint.data.bootstrapSecret },
  });
  if (!boot.ok()) throw new Error(`bootstrap failed: ${boot.status()} ${await boot.text()}`);
  // The CSRF token ships in a JS-readable cookie; the browser client reads it
  // the same way. State-changing calls must echo it in X-CSRF-Token.
  const csrf = (await api.storageState()).cookies.find((c) => c.name === 'cobot_csrf')?.value;
  if (!csrf) throw new Error('cobot_csrf cookie missing after bootstrap');
  const patch = await api.patch('/api/v1/settings', {
    headers: { 'X-CSRF-Token': csrf },
    data: { requestId: `e2e-engine-${Date.now()}`, patch: { enginePath: exe('fake-codex') } },
  });
  if (!patch.ok()) throw new Error(`enginePath patch failed: ${patch.status()} ${await patch.text()}`);
  await api.dispose();

  // Mint a fresh secret last: the specs redeem it, and the one-time secret
  // has a short TTL so it must not be issued before the engine is configured.
  const specSecretResp = await fetch(`${instance.endpoint}/internal/v1/session/link`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Instance-Secret': adminSecret },
    body: JSON.stringify({ requestId: `e2e-spec-${Date.now()}` }),
  });
  const specSecret = ((await specSecretResp.json()) as { data: { bootstrapSecret: string } }).data.bootstrapSecret;

  process.env.COBOT_E2E_BASE_URL = baseURL;
  process.env.COBOT_E2E_BOOTSTRAP = specSecret;
  process.env.COBOT_E2E_ENDPOINT = instance.endpoint;
  process.env.COBOT_E2E_ADMIN_SECRET = adminSecret;
  process.env.COBOT_E2E_ROOT = join(root, '.tmp', 'test-runs', runId);
  process.env.COBOT_E2E_TEAM_DIR = teamDir;
  process.env.COBOT_E2E_FAKE_ENGINE = exe('fake-codex');
  // The engine inherits the core's environment and polls FAKE_CONTROL before
  // each turn; specs switch behavior by rewriting that same file through the
  // COBOT_E2E_ alias so the helper and the engine agree on one path.
  const fakeControl = join(root, '.tmp', 'test-runs', runId, 'fake-mode');
  process.env.FAKE_CONTROL = fakeControl;
  process.env.COBOT_E2E_FAKE_CONTROL = fakeControl;
  process.env.COBOT_E2E_CORE_PID = String(child.pid ?? '');
  // Keep a handle for teardown through the environment (same process tree).
  (globalThis as any).__cobotE2EChild = child;
  (globalThis as any).__cobotE2EOutput = () => output;
}
