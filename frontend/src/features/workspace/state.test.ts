import { describe, expect, it } from 'vitest';
import {
  CHANGES_TAB_ID,
  DEFAULT_WIDTH,
  MAX_WIDTH,
  MIN_WIDTH,
  NEW_TAB_ID,
  clampWidth,
  initialWorkspaceState,
  loadWorkspace,
  saveWorkspace,
  workspaceKey,
  workspaceReducer,
  type WorkspaceTab,
} from './state';

const browserTab = (id: string, title = ''): WorkspaceTab => ({ id: `browser:${id}`, kind: 'browser', title, ref: id });
const fileTab = (path: string): WorkspaceTab => ({ id: `file:${path}`, kind: 'file', title: path, ref: path });

describe('workspaceReducer', () => {
  it('opens a new-tab tab and focuses it', () => {
    const state = workspaceReducer(initialWorkspaceState(), { type: 'newTab' });
    expect(state.open).toBe(true);
    expect(state.activeId).toBe(NEW_TAB_ID);
    expect(state.tabs).toHaveLength(1);
    // A second request focuses the same tab instead of stacking copies.
    const again = workspaceReducer(state, { type: 'newTab' });
    expect(again.tabs).toHaveLength(1);
    expect(again.activeId).toBe(NEW_TAB_ID);
  });

  it('shows a member page without stealing the view, but opens on the first tab', () => {
    // The user is on the new-tab page when a member opens its first page.
    let state = workspaceReducer(initialWorkspaceState(), { type: 'newTab' });
    state = workspaceReducer(state, { type: 'openTab', tab: browserTab('t1', '成员页') });
    expect(state.open).toBe(true);
    expect(state.tabs).toHaveLength(2);
    expect(state.activeId).toBe(NEW_TAB_ID);
    const withSecond = workspaceReducer(state, { type: 'openTab', tab: browserTab('t2', '另一页') });
    expect(withSecond.tabs).toHaveLength(3);
    expect(withSecond.activeId).toBe(NEW_TAB_ID);
    // An existing page's title refreshes in place.
    const renamed = workspaceReducer(withSecond, { type: 'openTab', tab: browserTab('t2', '新标题') });
    expect(renamed.tabs[2].title).toBe('新标题');
    expect(renamed.tabs).toHaveLength(3);
  });

  it('closes onto a neighbour and closes the workspace with the last tab', () => {
    let state = initialWorkspaceState();
    for (const tab of [fileTab('a.md'), fileTab('b.md'), { id: CHANGES_TAB_ID, kind: 'changes' as const, title: '变更', ref: '' }]) {
      state = workspaceReducer(state, { type: 'openTab', tab, focus: true });
    }
    state = workspaceReducer(state, { type: 'focus', id: 'file:b.md' });
    // Closing the active tab moves the view right onto the changes tab.
    state = workspaceReducer(state, { type: 'close', id: 'file:b.md' });
    expect(state.activeId).toBe(CHANGES_TAB_ID);
    // Closing a background tab leaves the view where it was.
    state = workspaceReducer(state, { type: 'close', id: 'file:a.md' });
    expect(state.activeId).toBe(CHANGES_TAB_ID);
    // The last close takes the workspace down with it.
    state = workspaceReducer(state, { type: 'close', id: CHANGES_TAB_ID });
    expect(state.open).toBe(false);
    expect(state.tabs).toHaveLength(0);
    expect(state.activeId).toBeNull();
  });

  it('collapses without closing the tabs and reopens on focus', () => {
    let state = workspaceReducer(initialWorkspaceState(), { type: 'openTab', tab: fileTab('a.md'), focus: true });
    state = workspaceReducer(state, { type: 'collapse' });
    expect(state.open).toBe(false);
    expect(state.tabs).toHaveLength(1);
    state = workspaceReducer(state, { type: 'focus', id: 'file:a.md' });
    expect(state.open).toBe(true);
  });

  it('clamps the width', () => {
    expect(clampWidth(10)).toBe(MIN_WIDTH);
    expect(clampWidth(99999)).toBe(MAX_WIDTH);
    expect(clampWidth(Number.NaN)).toBe(DEFAULT_WIDTH);
    const state = workspaceReducer(initialWorkspaceState(), { type: 'setWidth', width: 10_000 });
    expect(state.width).toBe(MAX_WIDTH);
  });

  it('ignores unknown ids', () => {
    const state = workspaceReducer(initialWorkspaceState(), { type: 'close', id: 'nope' });
    expect(state).toEqual(initialWorkspaceState());
  });
});

describe('workspace persistence', () => {
  it('round-trips a team and refuses junk', () => {
    const key = workspaceKey('team-1');
    let state = workspaceReducer(initialWorkspaceState(), { type: 'openTab', tab: browserTab('t7', '页面'), focus: true });
    state = workspaceReducer(state, { type: 'setWidth', width: 700 });
    saveWorkspace(key, state);
    const back = loadWorkspace(key);
    expect(back).toEqual(state);
    // Saved before the tab kinds were fixed: unreadable shapes are dropped.
    window.localStorage.setItem(key, JSON.stringify({ open: true, tabs: [{ id: 1 }] }));
    expect(loadWorkspace(key)).toBeNull();
    // A saved view of a tab that no longer exists falls back to the last tab.
    saveWorkspace(key, { open: true, width: DEFAULT_WIDTH, tabs: [browserTab('t1')], activeId: 'gone' });
    expect(loadWorkspace(key)?.activeId).toBe('browser:t1');
  });
});
