import { Client } from './client.mjs';
import path from 'node:path';
const c = new Client({ cwd: path.resolve('.tmp/app-server-spike') });
try {
  const init = await c.initialize();
  console.log('initialize', JSON.stringify({ platformOs: init.platformOs, platformFamily: init.platformFamily }));
  const a = await c.rpc('account/read', { refreshToken: false });
  console.log('account', JSON.stringify({ type: a.account?.type, requiresOpenaiAuth: a.requiresOpenaiAuth }));
  const models = await c.rpc('model/list', {});
  console.log('models', JSON.stringify(models.data?.map(m => ({ id: m.id, model: m.model, isDefault: m.isDefault }))));
  const t = await c.rpc('thread/start', { cwd: path.resolve('.tmp/app-server-spike'), sandbox: 'read-only', approvalPolicy: 'on-request', approvalsReviewer: 'user', config: { 'mcp_servers.node_repl.enabled': false, 'mcp_servers.rea.enabled': false }, developerInstructions: 'This is a bounded integration test. Do not delegate or inspect files outside the test directory.' });
  console.log('thread', JSON.stringify({ id: t.thread.id, model: t.model, modelProvider: t.modelProvider, cwd: t.cwd, sandbox: t.sandbox }));
} catch (e) { console.error('probe failed:', e.message); process.exitCode = 1; }
finally { await c.close(); }
