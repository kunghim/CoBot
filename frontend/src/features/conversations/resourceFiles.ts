import { api } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';
import { resourcePathKey } from './resources';

export type ResourceFile = { file: WorkspaceFile; target: string };

export async function currentFiles(scope: WorkspaceScope, targets: string[], signal: AbortSignal): Promise<Array<WorkspaceFile | null>> {
  const output: Array<WorkspaceFile | null> = Array(targets.length).fill(null);
  let position = 0;
  await Promise.all(Array.from({ length: Math.min(3, targets.length) }, async () => {
    while (position < targets.length) {
      signal.throwIfAborted();
      const index = position++;
      try { output[index] = await api.workspaceFile(scope, targets[index], signal); }
      catch { signal.throwIfAborted(); }
    }
  }));
  return output;
}

export async function resourceFiles(scope: WorkspaceScope, targets: string[], start: number, seen: string[], changed: string[], runId: string | undefined, signal: AbortSignal): Promise<{ files: ResourceFile[]; next: number }> {
  const keys = new Set([...seen, ...changed]);
  const output: ResourceFile[] = [];
  let cursor = start;
  while (cursor < targets.length && output.length < 20) {
    const batch = targets.slice(cursor, cursor + Math.min(3, 20 - output.length));
    const files = await currentFiles(scope, batch, signal);
    for (let index = 0; index < files.length; index++) {
      const file = files[index];
      if (!file) continue;
      const key = resourcePathKey(file.path);
      if (keys.has(key)) continue;
      keys.add(key);
      // Legacy turn-only records may not expose every recorded filename in
      // the reply's steps. Check the same authoritative historical lookup.
      if (runId) {
        try { await api.workspaceChangeDiff(scope, runId, file.path, signal); continue; }
        catch { signal.throwIfAborted(); }
      }
      output.push({ file, target: batch[index] });
    }
    cursor += batch.length;
  }
  return { files: output, next: cursor < targets.length ? cursor : -1 };
}
