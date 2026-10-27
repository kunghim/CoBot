import { test, expect } from '@playwright/test';
import { authorize } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

// The default-model picker (inside the providers section) must not tell
// a signed-in user to sign in.
//
// The model list comes from the engine, which answers in seconds rather than
// milliseconds: it starts a control server and asks Codex for its catalogue.
// While that is still in flight there is nothing to offer yet, but the user
// is not without models either — they are simply not loaded. Showing the
// "no models yet, please sign in" empty state during that window tells a
// signed-in user their account is missing, and invites them to sign in again.
//
// The engine's own answer is too fast to show the window reliably, so the
// spec holds the response back and looks at the page while it is in flight.
test('the default-model page waits for the engine instead of asking a signed-in user to sign in', async ({ page }) => {
  // The fake engine reports no account, so on this profile the page would
  // rightly say there is nothing to choose from. What this guards against is
  // what a *signed-in* user sees while the engine is still answering, so the
  // account state is the one thing the spec sets up itself.
  await page.route('**/api/v1/engine', async (route) => {
    const response = await route.fetch();
    const body = await response.json();
    if (body?.data) body.data.loggedIn = true;
    await route.fulfill({ status: response.status(), contentType: 'application/json', json: body });
  });

  // How long the catalogue is held back; the page is watched for the whole
  // of it, because a single look can miss a flash of the wrong state.
  const holdMs = 6000;

  // Hold the model catalogue back, the way a cold engine does.
  await page.route('**/api/v1/models', async (route) => {
    await new Promise((resolve) => setTimeout(resolve, holdMs));
    await route.continue();
  });

  await page.goto('/settings/providers');

  // Both wrong states live in the default-model card; the providers section
  // legitimately keeps a sign-in button on the ChatGPT account card beside it.
  const card = page.getByTestId('default-model-card');
  const emptyState = card.getByText(/还没有可选的模型|No model to choose yet/);
  const signInPrompt = card.getByText(/登录 ChatGPT 账号|sign in with ChatGPT/);

  // Watch the whole window the catalogue is in flight. The page may show a
  // placeholder; what it must never do is claim there is nothing to pick
  // from, or ask a signed-in user to sign in again.
  const deadline = Date.now() + holdMs - 500;
  let claimedEmpty = 0;
  let askedToSignIn = 0;
  while (Date.now() < deadline) {
    claimedEmpty = Math.max(claimedEmpty, await emptyState.count());
    askedToSignIn = Math.max(askedToSignIn, await signInPrompt.count());
    await page.waitForTimeout(200);
  }
  expect(claimedEmpty, 'the page must not claim there are no models while the catalogue is in flight').toBe(0);
  expect(askedToSignIn, 'the page must not ask a signed-in user to sign in while the catalogue is in flight').toBe(0);

  // Once the list arrives the engine's own model is on offer.
  const picker = page.getByRole('combobox').first();
  await expect(picker).toBeVisible({ timeout: 30000 });
  await expect(picker).toContainText('Fake Model', { timeout: 10000 });
});
