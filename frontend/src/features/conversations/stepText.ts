// Reading what a step holds: its fields and a tool's arguments.

export interface Phrase {
  key: string;
  params?: Record<string, string | number>;
}

export function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

/** Tool arguments arrive as an object, or as a JSON string from some engines. */
export function toolArguments(detail: Record<string, unknown>): Record<string, unknown> {
  const raw = detail.arguments;
  if (raw && typeof raw === 'object') return raw as Record<string, unknown>;
  if (typeof raw === 'string') {
    try {
      const parsed = JSON.parse(raw) as unknown;
      if (parsed && typeof parsed === 'object') return parsed as Record<string, unknown>;
    } catch {
      // not JSON: no arguments to describe
    }
  }
  return {};
}
