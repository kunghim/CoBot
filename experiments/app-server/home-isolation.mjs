import fs from 'node:fs/promises';
import path from 'node:path';
import os from 'node:os';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
import { Client, messageText } from './client.mjs';

// Installed 0.161.0 marks chatgptAuthTokens INTERNAL / UNSTABLE.
// Explicitly opted-in experiment, NOT a production authentication recommendation.
// Credentials stay in memory: never log RPC payloads or save auth/config values.
if (!process.argv.includes('--experimental-token-reuse')) {
  console.error('Requires --experimental-token-reuse; see the installed protocol warning.');
  process.exit(2);
}
const root = path.resolve('.tmp/app-server-spike/home-isolation-' + Date.now());
const nativeHome = path.resolve(process.env.CODEX_HOME || path.join(os.homedir(), '.codex'));
const homes = { production: path.join(root, 'production-codex'), development: path.join(root, 'development-codex') };
await fs.mkdir(root, { recursive: true });
for (const home of Object.values(homes)) await fs.mkdir(home);
const report = { startedAt: new Date().toISOString(), root, experimentalAuth: true, cases: [], privateThreadIds: [] };
const clients = [];
let stage = 'initialize';
const digest = async file => { try { return crypto.createHash('sha256').update(await fs.readFile(file)).digest('hex'); } catch (e) { if (e.code === 'ENOENT') return null; throw e; } };
const nativeAuthBefore = await digest(path.join(nativeHome, 'auth.json'));
const nativeConfigBefore = await digest(path.join(nativeHome, 'config.toml'));
async function auditCredentialFiles() {
  const auth = JSON.parse(await fs.readFile(path.join(nativeHome, 'auth.json'), 'utf8'));
  const needles = Object.values(auth.tokens || {}).filter(v => typeof v === 'string' && v.length > 80).map(v => Buffer.from(v));
  let filesChecked = 0, credentialValueMatches = 0;
  async function scan(dir) {
    for (const entry of await fs.readdir(dir, { withFileTypes: true })) {
      const file = path.join(dir, entry.name);
      if (entry.isDirectory()) await scan(file);
      else if (entry.isFile()) {
        const bytes = await fs.readFile(file); filesChecked++;
        if (needles.some(needle => bytes.includes(needle))) credentialValueMatches++;
      }
    }
  }
  await scan(root);
  return { filesChecked, credentialValueMatches };
}
const privateEnv = home => {
  const env = { ...process.env, CODEX_HOME: home, CODEX_SQLITE_HOME: home, RUST_LOG: 'off' };
  for (const key of ['OPENAI_API_KEY', 'CODEX_API_KEY', 'CODEX_ACCESS_TOKEN', 'ACCESS_TOKEN', 'OPENAI_ACCESS_TOKEN']) delete env[key];
  return env;
};
const newClient = home => {
  const c = new Client({ cwd: root, env: home ? privateEnv(home) : { ...process.env, RUST_LOG: 'off' }, args: home ? ['-c', 'cli_auth_credentials_store="ephemeral"', '-c', `sqlite_home=${JSON.stringify(home.replaceAll('\\', '/'))}`, '-c', 'features.prefer_mxc=true'] : [] });
  if (home) c.on('request', m => {
    // This probe does not implement renewal or tool approvals; fail explicitly,
    // instead of hanging or silently broadening the bounded conversation.
    c.send({ id: m.id, error: { code: -32601, message: 'Server request is outside this bounded isolation probe' } });
  });
  clients.push(c); return c;
};
const init = async c => { await c.rpc('initialize', { clientInfo: { name: 'cobot_isolation_probe', title: 'CoBot isolation experiment', version: '0.1.0' }, capabilities: { experimentalApi: true } }); c.notify('initialized'); };
const record = (name, facts) => { report.cases.push({ name, ...facts }); console.log(JSON.stringify({ name, ...facts })); };
async function listed(c, ids) {
  const found = new Set(); let cursor;
  for (let pages = 0; pages < 1000; pages++) {
    const r = await c.rpc('thread/list', { limit: 100, modelProviders: [], sourceKinds: ['cli', 'vscode', 'exec', 'appServer', 'unknown'], ...(cursor ? { cursor } : {}) });
    for (const t of r.data || []) if (ids.includes(t.id)) found.add(t.id);
    cursor = r.nextCursor;
    if (!cursor) return [...found];
  }
  throw new Error('Thread list pagination limit reached');
}
try {
  const native = newClient(); await init(native);
  const nativeAccount = await native.rpc('account/read', { refreshToken: false });
  assert.equal(nativeAccount.account?.type, 'chatgpt');
  record('native-account-type', { accountType: nativeAccount.account.type });
  const cfg = (await native.rpc('config/read', { includeLayers: false })).config;
  const providerId = cfg.model_provider || 'openai';
  // A fresh private home has no user MCP definitions. Adding disabled entries without
  // transport definitions would itself be invalid in this version.
  const safeConfig = { model: cfg.model };
  if (providerId !== 'openai') {
    const provider = cfg.model_providers?.[providerId]; assert.ok(provider);
    if (provider.base_url) {
      const url = new URL(provider.base_url); assert.ok(!url.username && !url.password && !url.search && !url.hash);
    }
    const safeProvider = {};
    for (const key of ['name', 'base_url', 'wire_api', 'requires_openai_auth', 'supports_websockets']) if (provider[key] !== undefined && provider[key] !== null) safeProvider[key] = provider[key];
    assert.equal(safeProvider.requires_openai_auth, true);
    safeConfig[`model_providers.${providerId}`] = safeProvider;
  }
  record('model-context', { model: cfg.model, provider: providerId, noProviderSecretsCopied: true });
  const importAuth = async c => {
    const auth = JSON.parse(await fs.readFile(path.join(nativeHome, 'auth.json'), 'utf8'));
    assert.ok(auth.tokens?.access_token && auth.tokens?.account_id);
    await c.rpc('account/login/start', { type: 'chatgptAuthTokens', accessToken: auth.tokens.access_token, chatgptAccountId: auth.tokens.account_id, chatgptPlanType: nativeAccount.account.planType });
    const a = await c.rpc('account/read', { refreshToken: false });
    assert.equal(a.account?.type, 'chatgpt'); assert.equal(a.account.email, nativeAccount.account.email);
    return { accountType: a.account.type, sameAccount: a.account.email === nativeAccount.account.email };
  };
  for (const [environment, home] of Object.entries(homes)) {
    stage = environment + '-initial-auth';
    let c = newClient(home); await init(c);
    const account = await c.rpc('account/read', { refreshToken: false });
    record(environment + '-independent-auth', { accountType: account.account?.type ?? null, existingLoginAutomaticallyReused: !!account.account });
    assert.equal(account.account, null);
    stage = environment + '-experimental-memory-auth';
    record(environment + '-experimental-memory-auth', await importAuth(c));
    stage = environment + '-real-turn';
    const t = await c.rpc('thread/start', { cwd: root, model: cfg.model, modelProvider: providerId, sandbox: 'read-only', approvalPolicy: 'on-request', approvalsReviewer: 'user', ephemeral: false, config: safeConfig, developerInstructions: 'This is a bounded CoBot isolation experiment. Do not use tools, delegate, inspect files, credentials or environment. Follow only the short conversation request.' });
    const id = t.thread.id; report.privateThreadIds.push(id);
    const marker = environment === 'production' ? 'COBOT_PRIVATE_PROD_7319' : 'COBOT_PRIVATE_DEV_5297';
    const turn = await c.turn(id, `Remember ${marker}. Reply only with this marker.`);
    assert.equal(turn.turn.status, 'completed'); assert.equal(messageText(turn.events).trim(), marker);
    const pathUnderPrivateHome = !!t.thread.path && path.resolve(t.thread.path).startsWith(path.resolve(home) + path.sep);
    record(environment + '-private-conversation', { threadId: id, pathUnderPrivateHome, turnStatus: turn.turn.status, markerMatched: messageText(turn.events).trim() === marker });
    assert.ok(pathUnderPrivateHome);
    await c.close();
    stage = environment + '-resume-after-restart';
    c = newClient(home); await init(c); await importAuth(c);
    const resumed = await c.rpc('thread/resume', { threadId: id, config: safeConfig, model: cfg.model, modelProvider: providerId, sandbox: 'read-only', approvalPolicy: 'on-request', approvalsReviewer: 'user' });
    const next = await c.turn(id, 'Without tools, reply only with the marker I asked you to remember in the previous message.');
    assert.equal(next.turn.status, 'completed'); assert.equal(messageText(next.events).trim(), marker);
    const ids = await listed(c, report.privateThreadIds);
    const authFileCreated = (await digest(path.join(home, 'auth.json'))) !== null;
    record(environment + '-persistent-resume', { sameThreadAfterRestart: resumed.thread.id === id, previousMarkerRemembered: messageText(next.events).trim() === marker, ownThreadListed: ids.includes(id), otherEnvironmentThreadListed: ids.some(x => x !== id), authFileCreated });
    assert.equal(resumed.thread.id, id); assert.ok(ids.includes(id)); assert.equal(ids.some(x => x !== id), false); assert.equal(authFileCreated, false);
    await c.close();
  }
  stage = 'official-login-flow';
  const loginHome = path.join(root, 'official-login-probe'); await fs.mkdir(loginHome);
  const loginClient = newClient(loginHome); await init(loginClient);
  const login = await loginClient.rpc('account/login/start', { type: 'chatgpt' });
  assert.equal(login.type, 'chatgpt'); assert.ok(login.loginId && login.authUrl);
  await loginClient.rpc('account/login/cancel', { loginId: login.loginId });
  const afterLogin = await loginClient.rpc('account/read', { refreshToken: false });
  record('official-login-flow', { browserLoginAvailable: !!login.authUrl, canceledWithoutSignIn: afterLogin.account === null, authFileCreated: (await digest(path.join(loginHome, 'auth.json'))) !== null });
  assert.equal(afterLogin.account, null);
  stage = 'native-list';
  const nativeFound = await listed(native, report.privateThreadIds);
  record('native-thread-list-isolation', { privateThreadsFound: nativeFound.length, passed: nativeFound.length === 0 });
  assert.equal(nativeFound.length, 0);
  for (const id of report.privateThreadIds) {
    let readable = false; try { await native.rpc('thread/read', { threadId: id }); readable = true; } catch {}
    assert.equal(readable, false);
  }
  const production = newClient(homes.production); await init(production);
  const finalProductionIds = await listed(production, report.privateThreadIds);
  record('production-development-isolation', { productionContainsOwnThread: finalProductionIds.includes(report.privateThreadIds[0]), productionContainsDevelopmentThread: finalProductionIds.includes(report.privateThreadIds[1]) });
  assert.ok(finalProductionIds.includes(report.privateThreadIds[0])); assert.equal(finalProductionIds.includes(report.privateThreadIds[1]), false);
  report.passed = true;
} catch (e) {
  report.passed = false;
  let errorCode = null; try { errorCode = JSON.parse(e.message)?.code ?? null; } catch {}
  // Never persist raw protocol errors, account identities, URLs or credential values.
  record('probe-failed', { stage, errorClass: e.name, errorCode });
  process.exitCode = 1;
} finally {
  for (const c of clients) await c.close();
  const authUnchanged = nativeAuthBefore === await digest(path.join(nativeHome, 'auth.json'));
  const configUnchanged = nativeConfigBefore === await digest(path.join(nativeHome, 'config.toml'));
  record('native-files-unchanged', { authUnchanged, configUnchanged });
  if (!authUnchanged || !configUnchanged) { report.passed = false; process.exitCode = 1; }
  report.credentialFileAudit = await auditCredentialFiles();
  record('credential-file-audit', report.credentialFileAudit);
  if (report.credentialFileAudit.credentialValueMatches) { report.passed = false; process.exitCode = 1; }
  report.finishedAt = new Date().toISOString();
  await fs.writeFile(path.join(root, 'report.json'), JSON.stringify(report, null, 2));
  console.log('REPORT ' + path.join(root, 'report.json'));
}
