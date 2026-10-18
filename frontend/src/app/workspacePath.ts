// The settings page leaves the workspace layout entirely; it returns to the
// last workspace location the user was on (team, member, query), not a
// generic home page.
let lastWorkspacePath = '/';

export function rememberWorkspacePath(path: string): void {
  if (!path.startsWith('/settings')) lastWorkspacePath = path;
}

export function workspacePath(): string {
  return lastWorkspacePath;
}
