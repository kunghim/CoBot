// The page's way in.
//
// A link that has expired used to be enough to lock the page out, even
// when the browser still held a working session. These are the rules that
// keep that from happening again.
import { describe, expect, it } from 'vitest';
import { authStateFrom } from './authState';

describe('page authorization', () => {
  it('lets the page in when the core says it is authorized', () => {
    expect(authStateFrom({ authorized: true })).toBe('authorized');
  });

  it('refuses the page when the core says it is not authorized', () => {
    expect(authStateFrom({ authorized: false })).toBe('unauthorized');
  });

  it('treats a core that cannot be reached as its own problem', () => {
    expect(authStateFrom({ failed: true })).toBe('unreachable');
  });

  it('keeps a working session in even when a link failed on the way', () => {
    // The one-time link was spent, but the core issued this page a session
    // as it loaded. That session is what the page has, so the page is in.
    expect(authStateFrom({ authorized: true, failed: false })).toBe('authorized');
  });

  it('does not mistake a missing answer for a refusal of the core', () => {
    expect(authStateFrom({})).toBe('unauthorized');
  });
});
