import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

test('create team, message leader, receive engine reply', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'team-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  // Every team gets its own engine, keyed by directory, so this spec's team
  // must never inherit the approval spec's blocking mode.
  setEngineMode('reply', teamDir);

  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();

  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await dialog.getByLabel(/团队名称|team name/i).fill('e2e团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();

  await expect(page.getByText('e2e团队').first()).toBeVisible({ timeout: 15000 });

  await expect(page.locator('textarea')).toBeVisible({ timeout: 15000 });

  // The team list is newest-first and earlier specs leave their own teams
  // behind, so scope the leader lookup to this team's rows. Collapsed teams
  // keep their bot buttons in the DOM but hidden, and strict mode plus
  // visibility filtering picks the one expanded team's leader.
  const leaders = page.getByRole('button', { name: /项目经理/ }).filter({ visible: true });
  await expect(leaders).toHaveCount(1, { timeout: 15000 });
  await leaders.first().click();

  const composer = page.locator('textarea').first();
  await composer.fill('请介绍一下你自己');
  await composer.press('Enter');

  await expect(page.getByText(/收到：/).first()).toBeVisible({ timeout: 20000 });
});
