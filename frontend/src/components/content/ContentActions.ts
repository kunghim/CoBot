// What shown content asks of the place around it: opening a file, showing a
// browser tab. The workspace answers; content knows nothing about it. Without
// a workspace around, a file says it cannot open and a web address leaves for
// a new browser tab.
import { createContext, useContext } from 'react';
import { ApiError } from '../../api/client';
import type { WorkspaceScope } from '../../api/types';

export interface ContentActions {
  openFile: (scope: WorkspaceScope, path: string) => Promise<void>;
  showBrowserTab: ((tab: { id: string; title: string }) => void) | null;
}

export const ContentActionsContext = createContext<ContentActions>({
  openFile: () => Promise.reject(new ApiError(503, 'capability_missing', 'error.workspace.unavailable', {})),
  showBrowserTab: null,
});

export const useContentActions = (): ContentActions => useContext(ContentActionsContext);
