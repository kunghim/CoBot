import { setTimeout as sleep } from 'node:timers/promises';

// Stops the core started by globalSetup; the throwaway data root stays under
// .tmp for post-mortem inspection and is never a production profile.
//
// The core is asked to stop the way the desktop shell asks it: on the way
// out it closes the team browsers it started. Killing the process instead
// skips that, and on Windows the headless browsers outlive it, holding the
// run's profile directories open. The kill stays as the fallback for a core
// that does not answer.
export default async function globalTeardown(): Promise<void> {
  if (process.env.COBOT_E2E_SKIP_SETUP === '1') return;
  const child = (globalThis as any).__cobotE2EChild;
  if (!child) return;
  const exited = new Promise<void>((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) resolve();
    else child.once('exit', () => resolve());
  });
  const endpoint = process.env.COBOT_E2E_ENDPOINT;
  const secret = process.env.COBOT_E2E_ADMIN_SECRET;
  let asked = false;
  if (endpoint && secret) {
    try {
      const resp = await fetch(`${endpoint}/internal/v1/service/stop`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Instance-Secret': secret },
        body: JSON.stringify({ requestId: `e2e-stop-${Date.now()}` }),
      });
      asked = resp.ok;
    } catch {
      /* the core is already gone or not answering; the kill below handles it */
    }
  }
  const stopped = asked && (await Promise.race([exited.then(() => true), sleep(15_000).then(() => false)]));
  if (!stopped) {
    child.kill();
    // Give the process a moment to release its instance lock cleanly.
    await sleep(500);
  }

  // The core's output is the only record of why an engine or dispatch failed,
  // so surface it instead of discarding it.
  const output = (globalThis as any).__cobotE2EOutput?.();
  if (output) {
    const safe = output.replace(/(#cobot-bootstrap=)[a-f0-9]+/g, '$1<redacted>');
    console.log('\n=== cobot core output (e2e) ===\n' + safe + '\n=== end core output ===\n');
  }
}
