import { describe, expect, it } from 'vitest';
import { parseNativeRequest } from './nativeRequest';

describe('parseNativeRequest', () => {
  it('reads a command request', () => {
    const request = parseNativeRequest(JSON.stringify({ command: 'pwsh -Command "npm install"', cwd: 'E:\\ws', network: true }));
    expect(request).toEqual(expect.objectContaining({ command: 'pwsh -Command "npm install"', cwd: 'E:\\ws', network: true, changes: [] }));
  });
  it('reads file changes whatever shape their kind has', () => {
    const request = parseNativeRequest(JSON.stringify({ changes: [{ path: 'a.md', kind: { type: 'add' }, diff: '+x' }, { path: 'b.md', kind: 'delete', diff: '' }] }));
    expect(request?.changes).toEqual([{ path: 'a.md', kind: 'add', diff: '+x' }, { path: 'b.md', kind: 'delete', diff: '' }]);
  });
  it('reads asked permissions', () => {
    const request = parseNativeRequest(JSON.stringify({ permissions: { network: { enabled: true }, fileSystem: { write: ['E:\\other'] } } }));
    expect(request?.permissions).toEqual({ network: true, read: [], write: ['E:\\other'], other: '' });
  });
  it('leaves everything else to the plain text view', () => {
    expect(parseNativeRequest('item/permissions/requestApproval')).toBeNull();
    expect(parseNativeRequest('approval.card.merge')).toBeNull();
    expect(parseNativeRequest('{"cwd":"E:\\\\ws"}')).toBeNull();
    expect(parseNativeRequest('{not json')).toBeNull();
  });
});
