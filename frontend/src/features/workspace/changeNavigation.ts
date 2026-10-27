import { api, ApiError } from '../../api/client';
import type { WorkspaceScope } from '../../api/types';
import { scopeKey } from './fileNavigation';

type Selection = { runId: string; botId: string; path?: string };
const listeners = new Map<string, (selection: Selection) => void>();
const requests = new Map<string, number>();

export function listenForChanges(scope: WorkspaceScope, listener: (selection: Selection) => void): () => void {
  const key = scopeKey(scope);
  listeners.set(key, listener);
  return () => { if (listeners.get(key) === listener) listeners.delete(key); };
}

export async function openWorkspaceChanges(scope: WorkspaceScope, runId: string, path?: string): Promise<void> {
  const key = scopeKey(scope);
  const sequence = (requests.get(key) ?? 0) + 1;
  requests.set(key, sequence);
  const result = path ? await api.workspaceChangeDiff(scope, runId, path) : await api.workspaceChangeFiles(scope, runId);
  if (requests.get(key) !== sequence) return;
  const listener = listeners.get(key);
  if (typeof listener !== 'function') throw new ApiError(503, 'capability_missing', 'error.workspace.unavailable', {});
  listener({ runId: result.run.id, botId: result.run.botId, path });
}
