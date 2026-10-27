import { useEffect, useRef, useState } from 'react';
import { ApiError, streamWorkspace } from '../../api/client';
import type { WorkspaceScope } from '../../api/types';
import { scopeKey } from './fileNavigation';

export function useWorkspaceWatch(scope: WorkspaceScope, paths: string[], retry: number, refresh: (resetPages: boolean) => void): boolean {
  const key = scopeKey(scope);
  const directories = JSON.stringify([...new Set(paths)].sort());
  const latest = useRef(refresh); latest.current = refresh;
  const [unavailable, setUnavailable] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout> | undefined;
    let attempt = 0;
    let connected = false;
    const run = async () => {
      if (controller.signal.aborted) return;
      if (JSON.parse(directories).length > 64) { setUnavailable(true); return; }
      try {
        const reason = await streamWorkspace(scope, JSON.parse(directories) as string[], (event) => {
          if (controller.signal.aborted) return;
          attempt = 0; setUnavailable(false); latest.current(event === 'changed' || connected); connected = true;
        }, controller.signal);
        if (controller.signal.aborted) return;
        if (reason === 'unavailable') setUnavailable(true);
      } catch (error) {
        if (controller.signal.aborted) return;
        setUnavailable(true);
        if (error instanceof ApiError && (error.code === 'capability_missing' || [400, 401, 403, 404, 503].includes(error.status))) return;
      }
      timer = setTimeout(() => void run(), Math.min(30000, 1000 * 2 ** Math.min(attempt++, 5)));
    };
    setUnavailable(false); void run();
    return () => { controller.abort(); clearTimeout(timer); };
  }, [key, directories, retry]);
  return unavailable;
}
