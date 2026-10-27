// The workspace's state: which tabs are open for one team, which is showing,
// how wide the area is. A pure reducer keeps the rules testable; the provider
// (WorkspaceProvider) keeps the state in localStorage, per team, on this
// machine only.

export type WorkspaceTabKind = 'new' | 'browser' | 'file' | 'changes';

export interface WorkspaceTab {
  /** Stable identity: "new", "browser:<panel tab id>", "file:<tab id>", "changes". */
  id: string;
  kind: WorkspaceTabKind;
  title: string;
  /** The panel tab id, the file path, or "" for the kinds that carry none. */
  ref: string;
  changesView?: { botId?: string; runId?: string; path?: string; listWidth?: number; listOpen?: boolean };
  fileView?: {
    expanded?: string[];
    treeWidth?: number;
    treeOpen?: boolean;
    hidden?: boolean;
    line?: number;
    column?: number;
    endLine?: number;
    scrollTop?: number;
    offset?: number;
    startLine?: number;
    positionId?: number;
  };
}

export interface WorkspaceState {
  open: boolean;
  width: number;
  tabs: WorkspaceTab[];
  activeId: string | null;
}

export const DEFAULT_WIDTH = 640;
export const MIN_WIDTH = 380;
export const MAX_WIDTH = 1200;
/** The one 新标签页 tab; the workspace shows it instead of a pane. */
export const NEW_TAB_ID = 'new';
/** The one 变更 tab. */
export const CHANGES_TAB_ID = 'changes';

export function initialWorkspaceState(): WorkspaceState {
  return { open: false, width: DEFAULT_WIDTH, tabs: [], activeId: null };
}

export type WorkspaceAction =
  | { type: 'updateTab'; id: string; patch: Partial<Pick<WorkspaceTab, 'title' | 'ref' | 'fileView' | 'changesView'>> }
  | { type: 'collapse' }
  | { type: 'setWidth'; width: number }
  | { type: 'newTab' }
  | { type: 'openTab'; tab: WorkspaceTab; focus?: boolean }
  | { type: 'focus'; id: string }
  | { type: 'close'; id: string };

export function clampWidth(width: number): number {
  if (!Number.isFinite(width)) return DEFAULT_WIDTH;
  return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, Math.round(width)));
}

function tabById(state: WorkspaceState, id: string): number {
  return state.tabs.findIndex((tab) => tab.id === id);
}

/** The tab that takes the place of a closed one: its right neighbour, else the left. */
function neighbour(tabs: WorkspaceTab[], index: number): string | null {
  if (tabs.length === 0) return null;
  // After the removal the right neighbour sits at the same index.
  const at = Math.min(index, tabs.length - 1);
  const next = tabs[at] ?? tabs[at - 1];
  return next ? next.id : null;
}

export function workspaceReducer(state: WorkspaceState, action: WorkspaceAction): WorkspaceState {
  switch (action.type) {
    case 'updateTab': {
      if (!state.tabs.some((tab) => tab.id === action.id)) return state;
      return { ...state, tabs: state.tabs.map((tab) => tab.id === action.id ? { ...tab, ...action.patch, fileView: { ...tab.fileView, ...action.patch.fileView }, changesView: { ...tab.changesView, ...action.patch.changesView } } : tab) };
    }
    case 'collapse':
      return { ...state, open: false };
    case 'setWidth': {
      const width = clampWidth(action.width);
      return width === state.width ? state : { ...state, width };
    }
    case 'newTab': {
      const existing = tabById(state, NEW_TAB_ID);
      if (existing >= 0) {
        if (state.open && state.activeId === NEW_TAB_ID) return state;
        return { ...state, open: true, activeId: NEW_TAB_ID };
      }
      const tab: WorkspaceTab = { id: NEW_TAB_ID, kind: 'new', title: '', ref: '' };
      return { ...state, open: true, tabs: [...state.tabs, tab], activeId: tab.id };
    }
    case 'openTab': {
      const { tab } = action;
      const existing = tabById(state, tab.id);
      // A page a member opened appears without stealing the view; the first
      // tab of all opens the workspace, and only then focuses itself.
      const first = state.tabs.length === 0;
      const focus = (action.focus ?? false) || first;
      if (existing >= 0) {
        const previous = state.tabs[existing];
        const title = tab.title || previous.title;
        if (title === previous.title && (!focus || (state.open && state.activeId === tab.id))) return state;
        const tabs = state.tabs.slice();
        // Refresh the stored title with what the page reports now.
        tabs[existing] = { ...previous, title };
        return {
          ...state,
          open: focus ? true : state.open,
          tabs,
          activeId: focus ? tab.id : state.activeId,
        };
      }
      return {
        ...state,
        open: first ? true : state.open,
        tabs: [...state.tabs, tab],
        activeId: focus ? tab.id : state.activeId,
      };
    }
    case 'focus': {
      const index = tabById(state, action.id);
      if (index < 0) return state;
      if (state.open && state.activeId === action.id) return state;
      return { ...state, open: true, activeId: action.id };
    }
    case 'close': {
      const index = tabById(state, action.id);
      if (index < 0) return state;
      const tabs = state.tabs.filter((tab) => tab.id !== action.id);
      // Closing the last tab closes the workspace; closing another moves the
      // view onto a neighbour.
      if (tabs.length === 0) return { ...state, tabs, open: false, activeId: null };
      const activeId = state.activeId === action.id ? neighbour(tabs, Math.min(index, tabs.length - 1)) : state.activeId;
      return { ...state, tabs, activeId };
    }
    default:
      return state;
  }
}

/** Reads a team's saved state, or null when there is none worth trusting. */
export function loadWorkspace(key: string): WorkspaceState | null {
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as Partial<WorkspaceState>;
    if (typeof parsed !== 'object' || parsed === null) return null;
    if (!Array.isArray(parsed.tabs)) return null;
    const valid = (tab: unknown): tab is WorkspaceTab =>
      typeof tab === 'object' && tab !== null &&
      typeof (tab as WorkspaceTab).id === 'string' &&
      ['new', 'browser', 'file', 'changes'].includes((tab as WorkspaceTab).kind) &&
      typeof (tab as WorkspaceTab).title === 'string' && typeof (tab as WorkspaceTab).ref === 'string';
    // One unreadable entry means the whole saved view is not trustworthy.
    if (!parsed.tabs.every(valid)) return null;
    const tabs = parsed.tabs.map((tab) => ({ ...tab, fileView: readFileView(tab.fileView), changesView: readChangesView(tab.changesView) }));
    const activeId = typeof parsed.activeId === 'string' && tabs.some((tab) => tab.id === parsed.activeId)
      ? parsed.activeId
      : (tabs.length > 0 ? tabs[tabs.length - 1].id : null);
    return {
      open: parsed.open === true && tabs.length > 0,
      width: clampWidth(typeof parsed.width === 'number' ? parsed.width : DEFAULT_WIDTH),
      tabs,
      activeId,
    };
  } catch {
    return null;
  }
}

function readChangesView(raw: unknown): WorkspaceTab['changesView'] {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return undefined;
  const value = raw as Record<string, unknown>;
  const string = (key: string, max: number) => typeof value[key] === 'string' && value[key].length <= max ? value[key] : undefined;
  return { botId: string('botId', 512), runId: string('runId', 512), path: string('path', 128 * 1024), listOpen: typeof value.listOpen === 'boolean' ? value.listOpen : undefined, listWidth: typeof value.listWidth === 'number' && Number.isFinite(value.listWidth) ? Math.round(Math.max(160, Math.min(400, value.listWidth))) : undefined };
}

function readFileView(raw: unknown): WorkspaceTab['fileView'] {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return undefined;
  const value = raw as Record<string, unknown>;
  const number = (key: string, min: number, max: number): number | undefined => typeof value[key] === 'number' && Number.isFinite(value[key]) ? Math.round(Math.min(max, Math.max(min, value[key]))) : undefined;
  return {
    expanded: Array.isArray(value.expanded) ? [...new Set(value.expanded.filter((path): path is string => typeof path === 'string' && path.length < 4096))].slice(0, 1024) : undefined,
    treeWidth: number('treeWidth', 140, 400),
    treeOpen: typeof value.treeOpen === 'boolean' ? value.treeOpen : undefined,
    hidden: typeof value.hidden === 'boolean' ? value.hidden : undefined,
    line: number('line', 1, Number.MAX_SAFE_INTEGER),
    column: number('column', 1, Number.MAX_SAFE_INTEGER),
    endLine: number('endLine', 1, Number.MAX_SAFE_INTEGER),
    scrollTop: number('scrollTop', 0, 100_000_000),
    offset: number('offset', 0, 256 * 1024 * 1024),
    startLine: number('startLine', 1, Number.MAX_SAFE_INTEGER),
    positionId: number('positionId', 0, Number.MAX_SAFE_INTEGER),
  };
}

export function saveWorkspace(key: string, state: WorkspaceState): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(state));
  } catch {
    // A full or blocked storage only costs the memory of the tabs.
  }
}

export function workspaceKey(teamId: string): string {
  return `cobot.workspace.v1.${teamId}`;
}

/** The room the conversation keeps beside the workspace. */
export const CONVERSATION_MIN_WIDTH = 420;
/** The icon rail and the drag line between conversation and workspace. */
const RAIL_WIDTH = 56;
const HANDLE_WIDTH = 8;

/** The team list's width at a viewport width (w-[min(16rem,40vw)]). */
export function teamListWidth(viewport: number): number {
  return Math.min(256, Math.round(viewport * 0.4));
}

export interface ColumnLayout {
  showList: boolean;
  showConversation: boolean;
  /** The workspace's drawn width; 0 when it is not drawn. */
  workspaceWidth: number;
}

/**
 * How the columns share a window. The conversation keeps its minimum; the
 * workspace narrows first, down to its own minimum; then the team list
 * steps out of the layout; and when even that is not enough, the
 * workspace takes the room and the conversation waits until it is folded.
 * The user's own choices (list open, workspace width) are kept: this only
 * decides what is drawn at this width.
 */
export function layoutColumns(viewport: number, listOpen: boolean, workspaceOpen: boolean, workspaceWidth: number): ColumnLayout {
  if (!workspaceOpen) return { showList: listOpen, showConversation: true, workspaceWidth: 0 };
  const room = (withList: boolean) => viewport - RAIL_WIDTH - (withList ? teamListWidth(viewport) : 0) - CONVERSATION_MIN_WIDTH - HANDLE_WIDTH;
  const wanted = clampWidth(workspaceWidth);
  if (listOpen && room(true) >= MIN_WIDTH) return { showList: true, showConversation: true, workspaceWidth: Math.min(wanted, room(true)) };
  if (room(false) >= MIN_WIDTH) return { showList: false, showConversation: true, workspaceWidth: Math.min(wanted, room(false)) };
  return { showList: false, showConversation: false, workspaceWidth: Math.max(0, viewport - RAIL_WIDTH - HANDLE_WIDTH) };
}
