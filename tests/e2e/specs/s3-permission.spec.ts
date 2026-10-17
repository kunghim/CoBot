import { test, expect, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

async function openNewTeam(page: Page, name: string, teamDir: string) {
  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill(name);
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await openLeader(page, name);
}

async function openLeader(page: Page, name: string) {
  const toggle = page.getByRole('button', { name: new RegExp(name) }).first();
  await expect(toggle).toBeVisible({ timeout: 15000 });
  if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
  await page.locator('li').filter({ has: toggle }).first().getByRole('button', { name: /队长/ }).first().click();
}

// The composer's permission switch belongs to the team: it starts at the
// default, full access asks first, and the choice survives a reload.
test('permission mode switches like Codex and is kept', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'perm-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  await openNewTeam(page, '权限团队', teamDir);

  const select = page.getByLabel('权限模式');
  await expect(select).toHaveValue('default', { timeout: 15000 });
  await expect(page.getByText('可在团队目录内自由读写和执行，超出范围时会问你')).toBeVisible();

  await select.selectOption('read_only');
  await expect(select).toHaveValue('read_only');

  await select.selectOption('full');
  const confirm = page.getByRole('alertdialog');
  await expect(confirm).toContainText('切换到完全访问？');
  await confirm.getByRole('button', { name: '切换到完全访问' }).click();
  await expect(select).toHaveValue('full');

  await page.reload();
  await openLeader(page, '权限团队');
  await expect(page.getByLabel('权限模式')).toHaveValue('full', { timeout: 15000 });
});

// A command approval reads as the command itself and offers what Codex
// offers: allow, allow for this session, decline, decline and stop.
test('a command approval offers the full Codex choices', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'perm-card-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  setEngineMode('approve', teamDir);
  await openNewTeam(page, '审批卡团队', teamDir);

  await page.locator('textarea').first().fill('跑一条命令');
  await page.locator('textarea').first().press('Enter');

  const card = page.getByTestId('native-approval').first();
  await expect(card).toBeVisible({ timeout: 20000 });
  await expect(card).toContainText('要运行命令');
  await expect(card.locator('pre')).toHaveText('Write-Host e2e');
  const section = page.locator('section').filter({ has: card }).first();
  for (const label of ['允许', '本会话都允许', '拒绝', '拒绝并停止']) {
    await expect(section.getByRole('button', { name: label, exact: true })).toBeVisible();
  }
  await section.getByRole('button', { name: '本会话都允许', exact: true }).click();
  await expect(card).toBeHidden({ timeout: 20000 });
  setEngineMode('reply', teamDir);
});
