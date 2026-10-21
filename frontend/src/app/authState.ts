// The page's way in.
//
// The core's answer is the authority. A one-time link that has expired or
// was already used is not a reason to lock the page out: the core may hold
// a session for this page all the same, and it says so. A core that cannot
// be reached is a different problem from a core that says no, and the two
// deserve different words.
export type AuthState = 'checking' | 'authorized' | 'unauthorized' | 'unreachable';

/** The core's answer, as the page reads it. */
export interface CoreAnswer {
  /** Whether the core holds a session for this page. */
  authorized?: boolean;
  /** Whether the core could not be reached at all. */
  failed?: boolean;
}

/** Turns what the core said into what the page shows. */
export function authStateFrom(answer: CoreAnswer): AuthState {
  if (answer.failed) return 'unreachable';
  return answer.authorized ? 'authorized' : 'unauthorized';
}
