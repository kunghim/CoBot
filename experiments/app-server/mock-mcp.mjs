// Minimal stdio MCP fixture. Caller identity comes from the host's launch args,
// never from model-supplied tool arguments. No real message delivery.
import fs from 'node:fs';
import { createInterface } from 'node:readline';
const [bot, log] = process.argv.slice(2);
if (!['alpha', 'beta'].includes(bot) || !log) throw new Error('Expected bot and log');
const respond = (id, result) => process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id, result }) + '\n');
createInterface({ input: process.stdin }).on('line', line => {
  let m; try { m = JSON.parse(line); } catch { return; }
  if (m.id === undefined) return;
  if (m.method === 'initialize') return respond(m.id, { protocolVersion: m.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'cobot_spike', version: '0.1.0' } });
  if (m.method === 'ping') return respond(m.id, {});
  if (m.method === 'tools/list') return respond(m.id, { tools: [{ name: 'send_message', description: 'Simulate sending a message to another bot. The server binds your identity; do not provide a sender.', inputSchema: { type: 'object', properties: { target: { type: 'string', enum: ['alpha', 'beta'] }, text: { type: 'string' } }, required: ['target', 'text'], additionalProperties: false }, annotations: { readOnlyHint: true, destructiveHint: false, openWorldHint: false } }] });
  if (m.method === 'tools/call') {
    const a = m.params.arguments || {};
    if (m.params.name !== 'send_message' || Object.keys(a).some(k => !['target', 'text'].includes(k)) || !['alpha', 'beta'].includes(a.target) || a.target === bot || typeof a.text !== 'string') return respond(m.id, { content: [{ type: 'text', text: 'Invalid message' }], isError: true });
    const receipt = { caller: bot, target: a.target, text: a.text, simulated: true };
    fs.appendFileSync(log, JSON.stringify(receipt) + '\n');
    return respond(m.id, { content: [{ type: 'text', text: JSON.stringify(receipt) }], isError: false });
  }
  process.stdout.write(JSON.stringify({ jsonrpc: '2.0', id: m.id, error: { code: -32601, message: 'Unknown method' } }) + '\n');
});
