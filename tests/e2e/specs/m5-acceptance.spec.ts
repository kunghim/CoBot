import { test, expect, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
  // These scenarios exercise teams; a fresh profile now opens in chat mode.
  await context.addInitScript(() => localStorage.setItem('cobot.mode', 'work'));
});

async function createTeam(
  page: Page,
  dirName: string,
  label: string,
  memberRoles: string[] = [],
): Promise<string> {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, dirName);
  mkdirSync(teamDir, { recursive: true });
  setEngineMode('reply', teamDir);
  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill(label);
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  for (const role of memberRoles) {
    await dialog.getByRole('checkbox', { name: new RegExp(role) }).check();
  }
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await expect(page.getByText(label).first()).toBeVisible({ timeout: 15000 });
  return teamDir;
}

async function selectMember(page: Page, name: RegExp) {
  const rows = page.getByRole('button', { name }).filter({ visible: true });
  await expect(rows.first()).toBeVisible({ timeout: 15000 });
  await rows.first().click();
}

async function sendCurrent(page: Page, text: string) {
  const composer = page.locator('textarea').first();
  await composer.fill(text);
  await composer.press('Enter');
}

// Three live turns raise three pending requests;
// paging moves between them without side effects, processing the current one
// shrinks the total, and the decided record stays in the read-only history.
test('three approval cards page, process, and keep history', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'm5-pager-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  setEngineMode('approve', teamDir);
  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill('多卡团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('checkbox', { name: /全栈工程师/ }).check();
  await dialog.getByRole('checkbox', { name: /测试工程师/ }).check();
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await expect(page.getByText('多卡团队').first()).toBeVisible({ timeout: 15000 });

  // One message per member: each thread enters approve mode and stays live,
  // so the team accumulates exactly three pending cards.
  await selectMember(page, /项目经理/);
  await sendCurrent(page, '给项目经理的请求');
  await selectMember(page, /全栈工程师/);
  await sendCurrent(page, '给后端的请求');
  await selectMember(page, /测试工程师/);
  await sendCurrent(page, '给测试的请求');

  // The leader conversation shows the whole team's pager.
  await selectMember(page, /项目经理/);
  const position = page.getByText(/\/ 3/).first();
  await expect(position).toContainText('1 / 3', { timeout: 20000 });

  // Paging: next/prev move the position without deciding anything.
  await page.getByRole('button', { name: /下一张审批卡|next approval/i }).first().click();
  await expect(position).toContainText('2 / 3');
  await page.getByRole('button', { name: /上一张审批卡|previous approval/i }).first().click();
  await expect(position).toContainText('1 / 3');

  // Processing the current card removes it and shrinks the total to two.
  const approve = page.getByRole('button').filter({ hasText: /^(允许|Allow)$/ }).filter({ visible: true });
  await approve.first().click();
  await expect(page.getByText(/\/ 3/)).toHaveCount(0, { timeout: 15000 });
  await expect(page.getByText(/\/ 2/).first()).toBeVisible({ timeout: 15000 });

  // The decided record lands in the read-only history, which lives in
  // the info card over the conversation.
  await page.getByRole('button', { name: /展开信息面板/ }).click();
  await page.getByRole('tab', { name: /^审批$/ }).click();
  await expect(page.getByText(/安全审批|Security approval/).first()).toBeVisible({ timeout: 15000 });

  // Cleanup: decide the rest so later specs see a quiet pager.
  for (let i = 0; i < 4; i++) {
    const again = page.getByRole('button').filter({ hasText: /^(允许|Allow)$/ }).filter({ visible: true });
    if ((await again.count()) === 0) break;
    await again.first().click();
    await page.waitForTimeout(400);
  }
  setEngineMode('reply', teamDir);
});

// Dangerous markup renders as inert text, never executes.
test('dangerous markup renders as inert text', async ({ page }) => {
  await createTeam(page, 'm5-xss-' + Date.now(), '注入测试');
  await selectMember(page, /项目经理/);
  await sendCurrent(page, '<img src=x onerror="window.__pwned=1"><script>window.__pwned=1</script>请原样显示');

  await expect(page.getByText('请原样显示').first()).toBeVisible({ timeout: 15000 });
  // The app header has a bundled brand image; messages may only show avatars.
  // Keep the injection check in the conversation and reject handlers everywhere.
  await expect(page.getByTestId('app-mode-header').locator('img')).toHaveCount(1);
  await expect(page.getByRole('main').locator('img:not([data-avatar])')).toHaveCount(0);
  await expect(page.locator('img[src="x"], img[onerror]')).toHaveCount(0);
  expect(await page.evaluate(() => (window as unknown as { __pwned?: number }).__pwned)).toBeUndefined();
});

// Only real actions on home, and arrow-key tab
// navigation with focus following the selection.
test('home shows only real actions and tabs support arrow keys', async ({ page }) => {
  await createTeam(page, 'm5-home-' + Date.now(), '首页诚实性');
  await expect(page.getByText(/即将|coming soon|待实现/i)).toHaveCount(0);
  await expect(page.getByRole('button', { name: /新建团队|create team/i })).toBeVisible();

  await selectMember(page, /项目经理/);
  // The four pages live in the info card; its tabs take the keys.
  await page.getByRole('button', { name: /展开信息面板/ }).click();
  const firstTab = page.getByRole('tab', { name: /^任务$/ });
  await firstTab.focus();
  await firstTab.press('ArrowRight');
  const secondTab = page.getByRole('tab', { name: /^审批$/ });
  await expect(secondTab).toHaveAttribute('aria-selected', 'true');
  await expect(secondTab).toBeFocused();
  await secondTab.press('End');
  await expect(page.getByRole('tab', { name: /记忆/ })).toHaveAttribute('aria-selected', 'true');
  await secondTab.press('Home');
  await expect(firstTab).toHaveAttribute('aria-selected', 'true');
});
