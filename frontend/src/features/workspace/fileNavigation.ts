import { api, ApiError } from '../../api/client';
import type { WorkspaceFile, WorkspaceScope } from '../../api/types';

const requests = new Map<string, number>();
const listeners = new Map<string, (file: WorkspaceFile) => void>();
export const scopeKey = (scope: WorkspaceScope) => `${scope.kind}:${scope.id}`;

/** File links work while the workspace column is collapsed. */
export function listenForFiles(scope: WorkspaceScope, listener: (file: WorkspaceFile) => void): () => void {
  const key = scopeKey(scope);
  listeners.set(key, listener);
  return () => { if (listeners.get(key) === listener) listeners.delete(key); };
}

export async function openWorkspaceFile(scope: WorkspaceScope, target: string): Promise<void> {
  const key = scopeKey(scope);
  const sequence = (requests.get(key) ?? 0) + 1;
  requests.set(key, sequence);
  const file = await api.workspaceFile(scope, target);
  if (requests.get(key) !== sequence) return;
  const listener = listeners.get(scopeKey(scope));
  if (!listener) throw new ApiError(503, 'capability_missing', 'error.workspace.unavailable', {});
  listener(file);
}
