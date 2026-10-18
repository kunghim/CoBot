import { createHash } from 'node:crypto';
import { mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { expect, request as pwRequest, type BrowserContext } from '@playwright/test';

// Redeems a freshly minted one-time startup secret for a page session. Each
// spec must mint its own: the product's startup secrets are single-use by
// design, so a shared one would only authorize the first spec.
export async function authorize(context: BrowserContext): Promise<void> {
  const endpoint = process.env.COBOT_E2E_ENDPOINT!;
  const adminSecret = process.env.COBOT_E2E_ADMIN_SECRET!;

  const admin = await pwRequest.newContext({
    baseURL: endpoint,
    extraHTTPHeaders: { 'X-Instance-Secret': adminSecret },
  });
  const mint = await admin.post('/internal/v1/session/link', {
    data: { requestId: `e2e-mint-${Date.now()}-${Math.random()}` },
  });
  expect(mint.ok(), await mint.text()).toBeTruthy();
  const { bootstrapSecret } = (await mint.json()).data;
  await admin.dispose();

  const api = await pwRequest.newContext({ baseURL: endpoint });
  const boot = await api.post('/api/v1/session/bootstrap', {
    data: { requestId: `e2e-boot-${Date.now()}-${Math.random()}`, secret: bootstrapSecret },
  });
  expect(boot.ok(), await boot.text()).toBeTruthy();
  await context.addCookies((await api.storageState()).cookies);
  await api.dispose();
}

// Drops a control file the running fake engine polls before each turn. Absent
// or empty means the engine's default "reply" mode, so a spec that never sets
// it can never inherit a mode left behind by an earlier spec.
//
// The file is keyed by the team's working directory, matching the engine: each
// team runs its own app-server against one shared core, so a single file would
// leak one team's mode into another team's turn.
export function setEngineMode(mode: 'reply' | 'approve' | 'deny' | 'fail' | 'process', teamDir: string): void {
  const base = process.env.COBOT_E2E_FAKE_CONTROL;
  if (!base) throw new Error('COBOT_E2E_FAKE_CONTROL is not set');
  mkdirSync(base, { recursive: true });
  const control = join(base, `mode-${modeKey(teamDir)}`);
  if (mode === 'reply') rmSync(control, { force: true });
  else writeFileSync(control, mode);
}

// modeKey matches the engine's cwd hash so both sides agree on one file name.
// The path is folded exactly as the engine folds it: forward slashes,
// lowercased on Windows, no trailing separator.
function modeKey(input: string): string {
  let p = input.replace(/\\/g, '/');
  if (process.platform === 'win32') p = p.toLowerCase();
  p = p.replace(/\/+$/, '');
  return createHash('sha256').update(p).digest('hex').slice(0, 16);
}
