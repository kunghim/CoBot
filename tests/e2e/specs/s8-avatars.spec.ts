import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

// The core downloads the avatar catalog at startup (from this checkout's
// assets in E2E); members then show their role's dango avatar.
test('members show their role avatar from the downloaded catalog', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'avatars-' + Date.now());
  mkdirSync(teamDir, { recursive: true });

  await page.goto('/settings/appearance');
  await expect(page.getByText(/团子：第 1 版|Dango: version 1/)).toBeVisible({ timeout: 15000 });

  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill('头像团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();

  const list = page.getByRole('complementary', { name: /团队列表|team list/i });
  const leader = list.locator('li').filter({ hasText: '头像团队' }).getByRole('button', { name: /队长/ }).first();
  await expect(leader).toBeVisible({ timeout: 15000 });
  const avatar = leader.locator('img').first();
  await expect(avatar).toHaveAttribute('src', /^data:image\/svg\+xml/);

  // Two members of different roles are drawn differently.
  const member = list.locator('li').filter({ hasText: '头像团队' }).getByRole('button', { name: /前端开发|Frontend/ }).first();
  const leaderSrc = await avatar.getAttribute('src');
  const memberSrc = await member.locator('img').first().getAttribute('src');
  expect(memberSrc).toMatch(/^data:image\/svg\+xml/);
  expect(memberSrc).not.toBe(leaderSrc);
});
