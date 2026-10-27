import { test, expect } from '@playwright/test';
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { authorize } from './helpers';

test.beforeEach(async ({ context }) => { await authorize(context); });

test('LaTeX keeps source readable and explains missing offline compiler resources', async ({ page }) => {
  test.skip(!!process.env.COBOT_LATEX_BINARY || !!process.env.COBOT_LATEX_CACHE, 'Exercises a core without provisioned resources');
  await page.goto('/chat/new');
  const response = page.waitForResponse((item) => item.url().endsWith('/api/v1/chats') && item.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('latex offline setup');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await response).json()).data;
  await expect(page.getByText('收到：latex offline setup', { exact: true })).toBeVisible();
  writeFileSync(join(created.chat.workspace, 'main.tex'), '\\documentclass{article}\n% OFFLINE SOURCE FIXTURE');
  await page.getByRole('button', { name: '创建标签页', exact: true }).click();
  await page.getByRole('button', { name: /文件 浏览当前工作区的文件/ }).click();
  await page.getByRole('treeitem', { name: 'main.tex', exact: true }).click();
  const workspace = page.getByRole('tabpanel', { name: '工作区' });
  await expect(workspace.getByText(/LaTeX 预览需要可用的隔离编译器和离线资源/)).toBeVisible();
  await expect(workspace.getByRole('button', { name: '编译 PDF', exact: true })).toBeDisabled();
  await expect(workspace.getByText('% OFFLINE SOURCE FIXTURE', { exact: true })).toBeVisible();
});

test('LaTeX selects the main file, renders a real PDF, retains it on errors and cancels compilation', async ({ page }) => {
  test.skip(process.platform !== 'darwin' || !process.env.COBOT_LATEX_BINARY || !process.env.COBOT_LATEX_CACHE, 'Requires explicitly provisioned macOS compiler resources');
  await page.goto('/chat/new');
  const response = page.waitForResponse((item) => item.url().endsWith('/api/v1/chats') && item.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('latex setup');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await response).json()).data;
  await expect(page.getByText('收到：latex setup', { exact: true })).toBeVisible();
  const main = join(created.chat.workspace, 'main.tex');
  const document = '\\documentclass{article}\n\\begin{document}\n\\input{part}\n\\end{document}';
  writeFileSync(main, document);
  writeFileSync(join(created.chat.workspace, 'part.tex'), 'LATEX PROJECT FIXTURE');
  writeFileSync(join(created.chat.workspace, 'other.tex'), '\\documentclass{article}\n\\begin{document}Other\\end{document}');
  await page.getByRole('button', { name: '创建标签页', exact: true }).click();
  await page.getByRole('button', { name: /文件 浏览当前工作区的文件/ }).click();
  await page.getByRole('treeitem', { name: 'part.tex', exact: true }).click();
  const workspace = page.getByRole('tabpanel', { name: '工作区' });
  await expect(workspace.getByRole('textbox', { name: '主文件', exact: true })).toHaveValue('');
  await expect(workspace.getByRole('button', { name: '编译 PDF', exact: true })).toBeDisabled();
  await workspace.getByRole('combobox', { name: '主文件候选', exact: true }).click();
  await page.getByRole('option', { name: 'main.tex', exact: true }).click();
  await workspace.getByRole('button', { name: '编译 PDF', exact: true }).click();
  await expect(workspace.locator('.pdf-text-layer')).toContainText('LATEX PROJECT FIXTURE');
  await workspace.getByRole('button', { name: '源码', exact: true }).click();
  await expect(workspace.locator('.pdf-text-layer')).toHaveCount(0);
  await workspace.getByRole('button', { name: 'PDF', exact: true }).click();
  await expect(workspace.locator('.pdf-text-layer')).toContainText('LATEX PROJECT FIXTURE');
  writeFileSync(main, '\\documentclass{article}\n\\begin{document}\\definitelyUndefinedCommand\\end{document}');
  await workspace.getByRole('button', { name: '编译 PDF', exact: true }).click();
  await expect(workspace.getByText(/编译失败，请查看日志并检查主文件/)).toBeVisible();
  await expect(workspace.getByText(/保留上次成功的 PDF/)).toBeVisible();
  await expect(workspace.locator('.pdf-text-layer')).toContainText('LATEX PROJECT FIXTURE');
  await expect(workspace.getByText('编译日志', { exact: true })).toBeVisible();
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-latex-error.png') });
  writeFileSync(main, '\\documentclass{article}\n\\begin{document}\\loop\\iftrue\\repeat\n\\end{document}');
  await workspace.getByRole('button', { name: '编译 PDF', exact: true }).click();
  await expect(workspace.getByRole('button', { name: '取消编译', exact: true })).toBeVisible();
  await workspace.getByRole('button', { name: '取消编译', exact: true }).click();
  await expect(workspace.getByRole('button', { name: '编译 PDF', exact: true })).toBeEnabled();
  writeFileSync(main, document);
  // A fresh successful request proves cancellation released the core's slot.
  await expect(async () => {
    await workspace.getByRole('button', { name: '编译 PDF', exact: true }).click();
    await expect(workspace.getByRole('button', { name: '编译 PDF', exact: true })).toBeVisible();
    await expect(workspace.getByText(/其他文档正在编译/)).toHaveCount(0);
  }).toPass({ timeout: 10_000 });
  await expect(workspace.getByText(/编译失败，请查看日志并检查主文件/)).toHaveCount(0);
  await expect(workspace.locator('.pdf-text-layer')).toContainText('LATEX PROJECT FIXTURE');
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-latex.png') });
});
