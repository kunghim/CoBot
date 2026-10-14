import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

// Switches the fake engine to "approve" mode: the next turn blocks on a native
// command approval, which must surface as a real card the user can decide.
// The engine inherits the core's env, so this is set on the running core
// process before the turn starts.
test('native tool approval appears, decides, engine resumes', async ({ page, context }) => {
  await authorize(context);

  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'approval-' + Date.now());
  mkdirSync(teamDir, { recursive: true });

  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByLabel(/团队名称|team name/i).fill('审批团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await expect(page.getByText('审批团队').first()).toBeVisible({ timeout: 15000 });

  await page.getByRole('button', { name: /队长/ }).first().click();

  // The engine switches to approval mode only for this turn, so the product
  // surfaces the card instead of the engine silently completing.
  setEngineMode('approve');

  await page.locator('textarea').first().fill('请执行一条命令');
  await page.locator('textarea').first().press('Enter');

  // The approval card must appear with a real command summary.
  await expect(page.getByText(/审批|approval/i).first()).toBeVisible({ timeout: 20000 });
  await expect(page.getByText(/Write-Host|pwsh/).first()).toBeVisible({ timeout: 20000 });

  // Decide once; the card must disappear and the engine must resume.
  await page.getByRole('button', { name: /允许|approve|allow/i }).first().click();
  await expect(page.getByText(/Write-Host|pwsh/).first()).toBeHidden({ timeout: 20000 });

  // Leave the shared engine back in its default so a later spec never blocks
  // on an approval nobody answers.
  setEngineMode('reply');
});
