// Reads the stored summary of a native engine approval: a JSON object with
// command/cwd/network, paths/changes, or permissions. Anything else (a
// product card's i18n key, a bare method name) is not a native request.

export interface NativeRequest {
  command?: string;
  cwd: string;
  network: boolean;
  paths: string[];
  changes: Array<{ path: string; kind: string; diff: string }>;
  permissions?: { network: boolean; read: string[]; write: string[]; other: string };
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === 'string') : [];
}

/** The change kind arrives as "add" or as {"type": "add", ...}. */
function changeKind(value: unknown): string {
  if (typeof value === 'string') return value;
  if (value && typeof value === 'object' && typeof (value as { type?: unknown }).type === 'string') return (value as { type: string }).type;
  return 'update';
}

export function parseNativeRequest(summary: string): NativeRequest | null {
  if (!summary.trimStart().startsWith('{')) return null;
  let data: Record<string, unknown>;
  try {
    data = JSON.parse(summary) as Record<string, unknown>;
  } catch {
    return null;
  }
  if (!data || typeof data !== 'object') return null;
  const changes = Array.isArray(data.changes)
    ? (data.changes as Array<Record<string, unknown>>).map((change) => ({
        path: typeof change.path === 'string' ? change.path : '',
        kind: changeKind(change.kind),
        diff: typeof change.diff === 'string' ? change.diff : '',
      }))
    : [];
  const request: NativeRequest = {
    command: typeof data.command === 'string' ? data.command : undefined,
    cwd: typeof data.cwd === 'string' ? data.cwd : '',
    network: data.network === true,
    paths: strings(data.paths),
    changes,
  };
  if (data.permissions && typeof data.permissions === 'object') {
    const asked = data.permissions as { network?: { enabled?: unknown } | null; fileSystem?: Record<string, unknown> | null };
    const fileSystem = asked.fileSystem ?? {};
    const read = strings(fileSystem.read);
    const write = strings(fileSystem.write);
    const entries = Array.isArray(fileSystem.entries) && fileSystem.entries.length > 0 ? JSON.stringify(fileSystem.entries, null, 2) : '';
    request.permissions = { network: asked.network?.enabled === true, read, write, other: entries };
  }
  if (request.command === undefined && changes.length === 0 && request.paths.length === 0 && !request.permissions) return null;
  return request;
}
