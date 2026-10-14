import { setTimeout as sleep } from 'node:timers/promises';

// Stops the core started by globalSetup; the throwaway data root stays under
// .tmp for post-mortem inspection and is never a production profile.
export default async function globalTeardown(): Promise<void> {
  if (process.env.COBOT_E2E_SKIP_SETUP === '1') return;
  const child = (globalThis as any).__cobotE2EChild;
  if (!child) return;
  child.kill();
  // Give the process a moment to release its instance lock cleanly.
  await sleep(500);

  // The core's output is the only record of why an engine or dispatch failed,
  // so surface it instead of discarding it.
  const output = (globalThis as any).__cobotE2EOutput?.();
  if (output) {
    console.log('\n=== cobot core output (e2e) ===\n' + output + '\n=== end core output ===\n');
  }
}
