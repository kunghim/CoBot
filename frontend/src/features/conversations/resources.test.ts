import { describe, expect, it } from 'vitest';
import { changedPaths, localAddress, replyResources, resourcePathKey } from './resources';

describe('reply resources', () => {
  it('collects explicit references and paths, excluding code blocks and remote sites', () => {
    const resources = replyResources('[source][s] ![image](images/demo.png) `src/app.ts:4` `.env`\n\n[s]: docs/readme.md\n\nhttps://localhost:3000/ https://example.com/\n\n```txt\nsecret/file.txt\n```');
    expect(resources).toEqual([
      { kind: 'file', target: 'docs/readme.md' }, { kind: 'file', target: 'images/demo.png' },
      { kind: 'file', target: 'src/app.ts:4' }, { kind: 'file', target: '.env' },
      { kind: 'site', target: 'https://localhost:3000/' },
    ]);
  });
  it('normalizes local addresses without accepting credentials or unrelated hosts', () => {
    expect(localAddress('http://[::1]:8080')).toBe('http://[::1]:8080/');
    expect(localAddress('http://a.localhost:3000')).toBe('http://a.localhost:3000/');
    expect(localAddress('https://localhost.example.com')).toBeUndefined();
    expect(localAddress('http://user:password@localhost')).toBeUndefined();
  });
  it('matches absolute workspace paths while preserving outside paths', () => {
    expect(resourcePathKey('/repo/docs/../readme.md', '/repo')).toBe('readme.md');
    expect(resourcePathKey('file:///repo/a%20b.md', '/repo')).toBe('a b.md');
    expect(resourcePathKey('/other/readme.md', '/repo')).not.toBe('readme.md');
    expect(resourcePathKey('src\\app.ts')).toBe('src/app.ts');
  });
  it('uses one complete turn snapshot rather than counting both event sources', () => {
    expect(changedPaths([
      { kind: 'file_change', status: 'completed', detail: { changes: [{ path: 'old.ts' }] } },
      { kind: 'diff', status: 'completed', detail: { filesComplete: true, changes: [{ path: 'before.ts', movePath: 'after.ts' }] } },
    ])).toEqual(['before.ts', 'after.ts']);
  });
});
