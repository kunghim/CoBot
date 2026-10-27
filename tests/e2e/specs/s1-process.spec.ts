import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

// The conversation shows what the member is doing while it works (Codex
// style) and keeps the process after the turn: reconnects, thinking, a
// command with its output, a file change, a Markdown reply whose file link
// is opened by the core.
test('a turn shows its process live and keeps it afterwards', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'process-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  setEngineMode('process', teamDir);

  await page.goto('/chat/new');
  await page.getByRole('radio', { name: '团队', exact: true }).click();
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill('过程团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await expect(page.getByText('过程团队').first()).toBeVisible({ timeout: 15000 });
  const leaders = page.getByRole('button', { name: /项目经理/ }).filter({ visible: true });
  await expect(leaders).toHaveCount(1, { timeout: 15000 });
  await leaders.first().click();

  const opened: Array<{ path: string; reveal?: boolean }> = [];
  await page.route('**/api/v1/teams/*/open-path', async (route) => {
    opened.push(JSON.parse(route.request().postData() ?? '{}'));
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: { path: 'notes.md' } }) });
  });

  const composer = page.locator('textarea').first();
  await composer.fill('跑一下测试');
  await composer.press('Enter');

  // While it works, the flow itself says what is happening.
  const runBlock = page.getByTestId('run-process').last();
  await expect(runBlock).toBeVisible({ timeout: 15000 });
  await expect(runBlock.getByRole('status')).toBeVisible();

  // Afterwards the reply is rendered Markdown and the process is one
  // collapsed line that expands into the same steps.
  await expect(page.getByRole('heading', { name: '已完成' })).toBeVisible({ timeout: 20000 });
  const header = runBlock.getByRole('button', { name: /已处理/ });
  await expect(header).toBeVisible({ timeout: 10000 });
  await expect(header).toHaveAttribute('aria-expanded', 'false');
  await header.click();
  await expect(runBlock.getByText('重新连接 1 次后恢复')).toBeVisible();
  await expect(runBlock.getByText('先看看目录结构')).toBeVisible();
  await expect(runBlock.getByText('运行了命令')).toBeVisible();
  await expect(runBlock.getByText('修改了 1 个文件')).toBeVisible();

  await runBlock.getByRole('button', { name: /运行了命令/ }).click();
  await expect(runBlock.getByText(/example\/pkg/)).toBeVisible();

  // The process survives a reload: it was stored, not just streamed.
  await page.reload();
  // The selected member and stored process are restored together.
  await expect(page.getByRole('heading', { name: '已完成' })).toBeVisible({ timeout: 15000 });
  await page.getByTestId('run-process').last().getByRole('button', { name: /已处理/ }).click();
  await expect(page.getByTestId('run-process').last().getByText('运行了命令')).toBeVisible();

  // A file link opens the internal file workspace and never launches a system program.
  await page.getByRole('link', { name: 'notes.md' }).click();
  const files = page.getByRole('tabpanel', { name: '工作区' });
  await expect(files.getByRole('heading', { name: '笔记', exact: true })).toBeVisible();
  await expect(files.getByRole('tree', { name: '项目文件' })).toBeVisible();
  expect(opened).toHaveLength(0);
  await page.getByRole('button', { name: '收起工作区', exact: true }).click();
  await page.getByRole('link', { name: 'notes.md' }).click();
  await expect(page.getByRole('tab', { name: 'notes.md', exact: true })).toHaveCount(1);
  await expect(files.getByRole('heading', { name: '笔记', exact: true })).toBeVisible();
  await expect(page).not.toHaveURL(/notes\.md/);

  await page.getByRole('button', { name: '新建标签页', exact: true }).click();
  await page.getByRole('button', { name: /变更 按轮次查看已记录的文件变更/ }).click();
  const changes = page.getByRole('tabpanel', { name: '工作区' });
  const history = changes.getByRole('complementary', { name: '变更记录', exact: true });
  await expect(history.getByRole('button').filter({ hasText: 'notes.md' })).toBeVisible();
  await history.getByRole('button').filter({ hasText: 'notes.md' }).click();
  await expect(changes.getByText('+# 笔记', { exact: true })).toBeVisible();
  await expect(history.getByText('+2', { exact: true }).first()).toBeVisible();
  await expect(changes.getByText(/当轮记录/)).toBeVisible();
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-changes-wide.png') });
  await changes.getByRole('button', { name: '打开当前文件', exact: true }).click();
  await expect(files.getByRole('heading', { name: '笔记', exact: true })).toBeVisible();
  await expect(page.getByRole('tab', { name: 'notes.md', exact: true })).toHaveCount(1);
  await page.getByRole('tab', { name: '变更', exact: true }).click();
  await page.reload();
  await expect(changes.getByText('+# 笔记', { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 440, height: 800 });
  await expect(changes.getByRole('button', { name: '展开变更列表', exact: true })).toBeVisible();
  await changes.getByRole('button', { name: '展开变更列表', exact: true }).click();
  await expect(history).toBeVisible();
  await history.getByRole('button').filter({ hasText: 'notes.md' }).click();
  await expect(changes.getByText('+# 笔记', { exact: true })).toBeVisible();
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-changes-narrow.png') });
  expect(opened).toHaveLength(0);

});
