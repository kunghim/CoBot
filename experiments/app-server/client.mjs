import { spawn } from 'node:child_process';
import { createInterface } from 'node:readline';
import { EventEmitter } from 'node:events';

export class Client extends EventEmitter {
  constructor({ cwd, binary = process.env.CODEX_BIN || 'codex', args = [], env = process.env }) {
    super();
    this.nextId = 1; this.pending = new Map(); this.events = [];
    const sandboxArgs = env.SPIKE_PREFER_MXC === '1' ? ['-c', 'features.prefer_mxc=true'] : [];
    this.child = spawn(binary, ['app-server', '--stdio', ...sandboxArgs, ...args], { cwd, env, windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] });
    this.stderr = '';
    this.child.stderr.on('data', b => { this.stderr += b.toString(); });
    this.child.on('error', e => this.fail(e));
    this.child.on('exit', (code, signal) => this.fail(new Error(`app-server exited: ${code}/${signal}`)));
    createInterface({ input: this.child.stdout }).on('line', line => {
      let msg; try { msg = JSON.parse(line); } catch { return; }
      if (msg.method) {
        this.events.push(msg); this.emit('event', msg);
        if (msg.id !== undefined) this.emit('request', msg);
      } else {
        const p = this.pending.get(msg.id);
        if (p) { clearTimeout(p.timer); this.pending.delete(msg.id); msg.error ? p.reject(new Error(JSON.stringify(msg.error))) : p.resolve(msg.result); }
      }
    });
  }
  fail(error) { for (const p of this.pending.values()) { clearTimeout(p.timer); p.reject(error); } this.pending.clear(); this.exitError = error; this.emit('closed', error); }
  send(msg) { this.child.stdin.write(JSON.stringify(msg) + '\n'); }
  notify(method, params = {}) { this.send({ method, params }); }
  reply(id, result) { this.send({ id, result }); }
  rpc(method, params = {}, timeout = 60000) {
    if (this.exitError) return Promise.reject(this.exitError);
    const id = this.nextId++;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error(`${method} timeout`)); }, timeout);
      this.pending.set(id, { resolve, reject, timer }); this.send({ id, method, params });
    });
  }
  async initialize() {
    const r = await this.rpc('initialize', { clientInfo: { name: 'cobot_spike', title: 'CoBot integration spike', version: '0.1.0' } });
    this.notify('initialized'); return r;
  }
  waitFor(predicate, timeout = 180000, from = 0) {
    const existing = this.events.slice(from).find(predicate);
    if (existing) return Promise.resolve(existing);
    return new Promise((resolve, reject) => {
      const finish = (e, value) => { clearTimeout(timer); this.off('event', onEvent); this.off('closed', onClose); e ? reject(e) : resolve(value); };
      const onEvent = m => { if (predicate(m)) finish(null, m); };
      const onClose = e => finish(e);
      const timer = setTimeout(() => finish(new Error('event timeout')), timeout);
      this.on('event', onEvent); this.on('closed', onClose);
    });
  }
  async turn(threadId, text, extra = {}) {
    const from = this.events.length;
    const r = await this.rpc('turn/start', { threadId, input: [{ type: 'text', text }], ...extra });
    const end = await this.waitFor(m => m.method === 'turn/completed' && m.params.threadId === threadId && m.params.turn.id === r.turn.id, 240000, from);
    return { turn: end.params.turn, events: this.events.slice(from).filter(m => m.params?.threadId === threadId) };
  }
  async close() { this.child.stdin.end(); await new Promise(r => setTimeout(r, 400)); if (this.child.exitCode === null) this.child.kill(); }
}

export const messageText = events => events.filter(m => m.method === 'item/completed' && m.params.item.type === 'agentMessage').map(m => m.params.item.text).join('\n');
export const nativeItems = events => events.filter(m => m.method === 'item/completed').map(m => m.params.item).filter(i => ['commandExecution', 'fileChange', 'mcpToolCall'].includes(i.type));
