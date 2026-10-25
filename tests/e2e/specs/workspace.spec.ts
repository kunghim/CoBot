import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
  // These scenarios exercise teams; a fresh profile now opens in chat mode.
  await context.addInitScript(() => localStorage.setItem('cobot.mode', 'work'));
});

// The workspace beside the conversation: the new-tab button opens it on its
// new-tab page, the user can open a browser page of their own, Esc closes
// the info card with its focus back on the button, and closing the last tab
// takes the workspace down and brings the button back.
test('workspace opens with tabs, the info card closes on Escape', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'workspace-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  setEngineMode('reply', teamDir);

  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill('工作区团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await expect(page.getByText('工作区团队').first()).toBeVisible({ timeout: 15000 });
  const leaders = page.getByRole('button', { name: /项目经理/ }).filter({ visible: true });
  await expect(leaders).toHaveCount(1, { timeout: 15000 });
  await leaders.first().click();

  // The new-tab button opens the workspace on its new-tab page, and stands
  // aside while the workspace is open.
  const create = page.getByRole('button', { name: '创建标签页' });
  await create.click();
  const workspace = page.getByRole('tablist', { name: '工作区' });
  await expect(workspace).toBeVisible();
  await expect(workspace.getByRole('tab', { name: '新建标签页' })).toHaveAttribute('aria-selected', 'true');
  await expect(create).toHaveCount(0);

  // If the workspace squeezes the list out, the conversation head follows
  // the actual conversation edge. Expanding the list gives it room again.
  await page.setViewportSize({ width: 900, height: 900 });
  const toggle = page.getByTestId('app-title-bar').getByRole('button', { name: '展开团队列表', exact: true });
  await expect(toggle).toBeVisible();
  // The squeeze-out follows a resize listener; the geometry settles async.
  await expect.poll(async () => Math.abs((await toggle.boundingBox())!.x - (await page.getByRole('main').boundingBox())!.x)).toBeLessThan(1);
  await toggle.click();
  await expect(page.getByRole('complementary', { name: '团队列表', exact: true })).toBeVisible();
  await expect(workspace).toHaveCount(0);
  await page.setViewportSize({ width: 1440, height: 900 });
  await create.click();
  await expect(workspace).toBeVisible();

  // The info card floats over the conversation; Escape closes it and the
  // focus returns to the button that opened it.
  const info = page.getByRole('button', { name: '展开信息面板' });
  await info.click();
  const card = page.getByRole('region', { name: '信息面板' });
  await expect(card).toBeVisible();
  await expect(card.getByRole('tablist', { name: '工作面板页面' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(card).toHaveCount(0);
  await expect(info).toBeFocused();

  // A browser page of the user's own: the new-tab page opens one, the tab
  // shows the page, the viewport row and the shared status line. The
  // browser takes a moment to come up.
  await page.getByRole('button', { name: /打开一个网页标签/ }).click();
  await expect(workspace.getByRole('tab', { name: /空白页|about:blank/ })).toHaveAttribute('aria-selected', 'true', { timeout: 20000 });
  await expect(page.getByText('成员需要时可以查看')).toBeVisible({ timeout: 20000 });
  await expect(page.getByRole('combobox', { name: /视口大小/ })).toBeVisible();

  // Closing the last tab takes the workspace down; the button returns.
  await workspace.getByRole('button', { name: /关闭 (空白页|about:blank)/ }).click();
  await expect(workspace).toHaveCount(0);
  await expect(page.getByRole('button', { name: '创建标签页' })).toBeVisible();
});
