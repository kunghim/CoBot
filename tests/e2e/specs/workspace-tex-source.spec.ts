import { test, expect } from '@playwright/test';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { authorize } from './helpers';

test.beforeEach(async ({ context }) => { await authorize(context); });

// TeX files have no dedicated preview: they open as plain text in the source
// viewer, without any compile controls.
test('a .tex file opens in the source viewer', async ({ page }) => {
  await page.goto('/chat/new');
  const response = page.waitForResponse((item) => item.url().endsWith('/api/v1/chats') && item.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('tex source setup');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await response).json()).data;
  await expect(page.getByText('收到：tex source setup', { exact: true })).toBeVisible();
  writeFileSync(join(created.chat.workspace, 'main.tex'), '\\documentclass{article}\n% TEX SOURCE FIXTURE');
  await page.getByRole('button', { name: '创建标签页', exact: true }).click();
  await page.getByRole('button', { name: /文件 浏览当前工作区的文件/ }).click();
  await page.getByRole('treeitem', { name: 'main.tex', exact: true }).click();
  const workspace = page.getByRole('tabpanel', { name: '工作区' });
  const source = workspace.getByLabel('main.tex', { exact: true });
  await expect(source.getByText('% TEX SOURCE FIXTURE', { exact: true })).toBeVisible();
  await expect(workspace.getByRole('button', { name: '编译 PDF' })).toHaveCount(0);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-tex-source.png') });
});
