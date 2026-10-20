// The workspace's place in the shell: one state per team, kept in this
// machine's localStorage, plus the info card over the conversation — which
// belongs to the member in view and closes when the view switches.
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import {
  initialWorkspaceState,
  loadWorkspace,
  saveWorkspace,
  workspaceKey,
  workspaceReducer,
  type WorkspaceAction,
  type WorkspaceState,
  type WorkspaceTab,
} from './state';

interface WorkspaceContext {
  /** The team the workspace shows; null without a member in view. */
  teamId: string | null;
  state: WorkspaceState;
  newTab: () => void;
  openTab: (tab: WorkspaceTab, focus?: boolean) => void;
  focusTab: (id: string) => void;
  closeTab: (id: string) => void;
  collapse: () => void;
  setWidth: (width: number) => void;
  /** The info card over the conversation, for the member in view. */
  cardOpen: boolean;
  setCardOpen: (open: boolean) => void;
}

const WorkspaceContext = createContext<WorkspaceContext | null>(null);

export function WorkspaceProvider({ teamId, botId, children }: { teamId: string | null; botId: string | null; children: ReactNode }) {
  const [entry, setEntry] = useState<{ teamId: string | null; state: WorkspaceState }>(() => ({
    teamId,
    state: teamId ? loadWorkspace(workspaceKey(teamId)) ?? initialWorkspaceState() : initialWorkspaceState(),
  }));
  // Switching teams saves the old team's view and restores the new one's.
  useEffect(() => {
    setEntry((previous) => {
      if (previous.teamId === teamId) return previous;
      if (previous.teamId) saveWorkspace(workspaceKey(previous.teamId), previous.state);
      return { teamId, state: teamId ? loadWorkspace(workspaceKey(teamId)) ?? initialWorkspaceState() : initialWorkspaceState() };
    });
  }, [teamId]);

  const dispatch = useCallback((action: WorkspaceAction) => {
    setEntry((previous) => {
      if (!previous.teamId) return previous;
      const state = workspaceReducer(previous.state, action);
      saveWorkspace(workspaceKey(previous.teamId), state);
      return { teamId: previous.teamId, state };
    });
  }, []);

  // The card belongs to the conversation being read; another member means
  // another conversation, so it closes itself.
  const [cardOpen, setCardOpen] = useState(false);
  useEffect(() => setCardOpen(false), [botId]);

  const value = useMemo<WorkspaceContext>(
    () => ({
      teamId: entry.teamId,
      state: entry.state,
      newTab: () => dispatch({ type: 'newTab' }),
      openTab: (tab, focus) => dispatch({ type: 'openTab', tab, focus }),
      focusTab: (id) => dispatch({ type: 'focus', id }),
      closeTab: (id) => dispatch({ type: 'close', id }),
      collapse: () => dispatch({ type: 'collapse' }),
      setWidth: (width) => dispatch({ type: 'setWidth', width }),
      cardOpen,
      setCardOpen,
    }),
    [entry, dispatch, cardOpen],
  );

  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

export function useWorkspace(): WorkspaceContext {
  const value = useContext(WorkspaceContext);
  if (!value) throw new Error('useWorkspace must be used inside WorkspaceProvider');
  return value;
}
