import path from 'node:path';
import fs from 'node:fs';
import { Client } from './client.mjs';
const root = path.resolve(process.argv[2]);
const c = new Client({ cwd: root });
try {
  await c.initialize();
  for (const bot of process.argv.length > 3 ? process.argv.slice(3) : ['alpha', 'beta']) {
    const list = await c.rpc('thread/list', { cwd: path.join(root, bot), limit: 10, sourceKinds: [] });
    for (const t of list.data || []) {
      const r = await c.rpc('thread/read', { threadId: t.id, includeTurns: true });
      const turns = r.thread.turns;
      fs.writeFileSync(path.join(root, 'inspect-' + t.id + '.json'), JSON.stringify({ threadId: t.id, turns }, null, 2));
      console.log(bot, t.id, JSON.stringify(turns));
    }
  }
} finally { await c.close(); }
