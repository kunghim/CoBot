/** The next segment of a validated selected path beneath a canonical directory.
 * This lets the tree reveal one selected entry without fetching every page. */
export function selectedChild(directory: string, selected: string, names: string[]): { path: string; name: string } | undefined {
  const prefix = directory ? directory + '/' : '';
  if (!selected.startsWith(prefix) || selected === directory) return;
  const name = selected.slice(prefix.length).split('/')[0];
  if (!name || name === '.' || name === '..' || names.includes(name)) return;
  return { path: prefix + name, name };
}
