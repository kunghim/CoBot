// A tiny bus for "show this in the work panel": an activity entry asks for
// a tab (and a task to open); the work panel and the layout listen. No state
// lives here beyond the last request, which the panel consumes.
import { useEffect } from 'react';

type WorkPanelTab = 'tasks' | 'approvals' | 'activity' | 'memory';
export interface WorkPanelRequest {
  tab: WorkPanelTab;
  taskId?: string;
  nonce: number;
}

const listeners = new Set<(request: WorkPanelRequest) => void>();
let nonce = 0;
// A collapsed panel is not mounted; it takes the request when it opens.
let pending: WorkPanelRequest | null = null;

export function showInWorkPanel(tab: WorkPanelTab, taskId?: string): void {
  const request = { tab, taskId, nonce: ++nonce };
  pending = request;
  for (const listener of listeners) listener(request);
}

/** The request no open panel has handled yet, once. */
export function takePendingWorkPanelRequest(): WorkPanelRequest | null {
  const request = pending;
  pending = null;
  return request;
}

export function useWorkPanelRequests(listener: (request: WorkPanelRequest) => void): void {
  useEffect(() => {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  }, [listener]);
}
