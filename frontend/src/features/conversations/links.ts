// Classifies a link a member wrote. Web links open in the browser; a path
// (absolute, drive-letter, file://, or relative) goes to the core to open
// inside the team folder; every other scheme (javascript:, data:, ...) is
// blocked.
type LinkTarget = { kind: 'web' } | { kind: 'local'; path: string } | { kind: 'blocked' };

export function linkTarget(href: string): LinkTarget {
  const value = href.trim();
  if (value === '' || value.startsWith('#')) return { kind: 'blocked' };
  if (/^(https?:|mailto:)/i.test(value)) return { kind: 'web' };
  if (/^file:/i.test(value)) return { kind: 'local', path: value };
  // A drive letter ("E:\\x", "E:/x", "/E:/x") is a path, not a scheme.
  if (/^\/?[a-z]:[\\/]/i.test(value)) return { kind: 'local', path: value };
  if (/^[a-z][a-z0-9+.-]*:/i.test(value)) return { kind: 'blocked' };
  if (value.startsWith('//')) return { kind: 'blocked' };
  return { kind: 'local', path: value };
}
