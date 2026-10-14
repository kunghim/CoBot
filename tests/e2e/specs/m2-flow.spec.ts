import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
  // Specs share one engine process, so an earlier approval spec could leave it
  // in a mode that blocks this turn. This spec expects a plain reply.
  setEngineMode('reply');
});

test('create team, message leader, receive engine reply', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'team-' + Date.now());
  mkdirSync(teamDir, { recursive: true });

  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByLabel(/团队名称|team name/i).fill('e2e团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();

  await expect(page.getByText('e2e团队').first()).toBeVisible({ timeout: 15000 });

  const leader = page.getByRole('button', { name: /队长/ }).first();
  await leader.click();

  const composer = page.locator('textarea').first();
  await composer.fill('请介绍一下你自己');
  await composer.press('Enter');

  await expect(page.getByText(/收到：/).first()).toBeVisible({ timeout: 20000 });
});
