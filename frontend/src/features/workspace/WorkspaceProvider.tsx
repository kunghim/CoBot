// The workspace belongs to a team or a saved chat. File navigation remains
// mounted while the workspace column is collapsed.
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import type { WorkspaceScope } from '../../api/types';
import { useWorkPanelRequests } from '../../app/workPanelNav';
import { listenForChanges } from './changeNavigation';
import { CHANGES_TAB_ID } from './state';
import { listenForFiles } from './fileNavigation';
import { initialWorkspaceState, loadWorkspace, saveWorkspace, workspaceKey, workspaceReducer, type WorkspaceAction, type WorkspaceState, type WorkspaceTab } from './state';

interface WorkspaceContext {
  teamId: string | null;
  botId: string | null;
  scope: WorkspaceScope | null;
  state: WorkspaceState;
  newTab: () => void;
  newFile: () => void;
  openTab: (tab: WorkspaceTab, focus?: boolean) => void;
  updateTab: (id: string, patch: Partial<Pick<WorkspaceTab, 'title' | 'ref' | 'fileView' | 'changesView'>>) => void;
  focusTab: (id: string) => void;
  closeTab: (id: string) => void;
  collapse: () => void;
  toggleFullView: () => void;
  setWidth: (width: number) => void;
  cardOpen: boolean;
  setCardOpen: (open: boolean) => void;
}
const WorkspaceContext = createContext<WorkspaceContext | null>(null);

export function WorkspaceProvider({ teamId = null, chatId = null, botId = null, children }: { teamId?: string | null; chatId?: string | null; botId?: string | null; children: ReactNode }) {
  const scope = useMemo<WorkspaceScope | null>(() => teamId ? { kind: 'team', id: teamId } : chatId ? { kind: 'chat', id: chatId } : null, [teamId, chatId]);
  // Keep the original team key so existing browser tabs and widths survive.
  const key = scope ? workspaceKey(scope.kind === 'team' ? scope.id : `chat:${scope.id}`) : null;
  const [entry, setEntry] = useState<{ key: string | null; state: WorkspaceState }>(() => ({ key, state: key ? loadWorkspace(key) ?? initialWorkspaceState() : initialWorkspaceState() }));
  useEffect(() => {
    setEntry((previous) => previous.key === key ? previous : { key, state: key ? loadWorkspace(key) ?? initialWorkspaceState() : initialWorkspaceState() });
  }, [key]);
  const dispatch = useCallback((action: WorkspaceAction) => {
    setEntry((previous) => {
      if (!key || previous.key !== key) return previous;
      const state = workspaceReducer(previous.state, action);
      if (state === previous.state) return previous;
      saveWorkspace(key, state);
      return { key, state };
    });
  }, [key]);

  const positionId = useRef(0);
  useEffect(() => {
    if (!scope || !key) return;
    return listenForFiles(scope, (file) => {
      const position = ++positionId.current;
      setEntry((previous) => {
        if (previous.key !== key) return previous;
        const existing = previous.state.tabs.find((tab) => tab.kind === 'file' && tab.ref === file.path);
        const tab: WorkspaceTab = existing ?? { id: `file:${crypto.randomUUID()}`, kind: 'file', title: file.name, ref: file.path };
        let state = workspaceReducer(previous.state, { type: 'openTab', tab, focus: true });
        state = workspaceReducer(state, { type: 'updateTab', id: tab.id, patch: { fileView: { positionId: position, line: file.line, column: file.column, endLine: file.endLine, ...(file.line ? { offset: 0, startLine: 1, scrollTop: 0 } : {}) } } });
        saveWorkspace(key, state);
        return { key, state };
      });
    });
  }, [scope, key]);

  useEffect(() => {
    if (!scope || !key) return;
    return listenForChanges(scope, (selection) => {
      setEntry((previous) => {
        if (previous.key !== key) return previous;
        let state = workspaceReducer(previous.state, { type: 'openTab', tab: { id: CHANGES_TAB_ID, kind: 'changes', title: '', ref: '' }, focus: true });
        state = workspaceReducer(state, { type: 'updateTab', id: CHANGES_TAB_ID, patch: { changesView: { botId: selection.botId || undefined, runId: selection.runId, path: selection.path } } });
        saveWorkspace(key, state);
        return { key, state };
      });
    });
  }, [scope, key]);

  const [cardOpen, setCardOpen] = useState(false);
  useEffect(() => setCardOpen(false), [botId, chatId]);
  useWorkPanelRequests(useCallback(() => setCardOpen(true), []));
  const state = entry.key === key ? entry.state : initialWorkspaceState();
  const value = useMemo<WorkspaceContext>(() => ({
    teamId, botId, scope, state,
    newTab: () => dispatch({ type: 'newTab' }),
    newFile: () => dispatch({ type: 'openTab', tab: { id: `file:${crypto.randomUUID()}`, kind: 'file', title: '', ref: '' }, focus: true }),
    openTab: (tab, focus) => dispatch({ type: 'openTab', tab, focus }),
    updateTab: (id, patch) => dispatch({ type: 'updateTab', id, patch }),
    focusTab: (id) => dispatch({ type: 'focus', id }),
    closeTab: (id) => dispatch({ type: 'close', id }),
    collapse: () => dispatch({ type: 'collapse' }),
    toggleFullView: () => dispatch({ type: 'toggleFullView' }),
    setWidth: (width) => dispatch({ type: 'setWidth', width }),
    cardOpen, setCardOpen,
  }), [teamId, botId, scope, state, dispatch, cardOpen]);
  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

export function useWorkspace(): WorkspaceContext {
  const value = useContext(WorkspaceContext);
  if (!value) throw new Error('useWorkspace must be used inside WorkspaceProvider');
  return value;
}
