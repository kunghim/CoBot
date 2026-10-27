import { test, expect } from '@playwright/test';
import { readFileSync, truncateSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, selectWorkspaceFile } from './helpers';

const images = JSON.parse(readFileSync(new URL('../fixtures/file-images.json', import.meta.url), 'utf8')) as Array<{ extension: string; width: number; height: number; frames: number; data: string }>;
test.beforeEach(async ({ context }) => { await authorize(context); });

test('image formats decode real pixels and reject damaged, empty and oversized files', async ({ page }) => {
  await page.goto('/chat/new');
  const response = page.waitForResponse((item) => item.url().endsWith('/api/v1/chats') && item.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('image matrix setup');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await response).json()).data;
  await expect(page.getByText('收到：image matrix setup', { exact: true })).toBeVisible();
  for (const image of images) {
    writeFileSync(join(created.chat.workspace, 'fixture.' + image.extension), Buffer.from(image.data, 'base64'));
    writeFileSync(join(created.chat.workspace, 'damaged.' + image.extension), 'not an image');
    writeFileSync(join(created.chat.workspace, 'empty.' + image.extension), '');
  }
  const large = join(created.chat.workspace, 'oversized.png');
  writeFileSync(join(created.chat.workspace, ' fixture.png'), Buffer.from(images.find((item) => item.extension === 'png')!.data, 'base64'));
  writeFileSync(large, ''); truncateSync(large, 257 * 1024 * 1024);
  writeFileSync(join(created.chat.workspace, 'unsupported.tiff'), Buffer.from(images.find((item) => item.extension === 'png')!.data, 'base64'));
  const content: string[] = [];
  page.on('request', (request) => { const url = new URL(request.url()); if (url.pathname.endsWith('/content')) content.push(url.searchParams.get('path') ?? ''); });
  await page.getByRole('button', { name: '创建标签页', exact: true }).click();
  await page.getByRole('button', { name: /文件 浏览当前工作区的文件/ }).click();
  const workspace = page.getByRole('tabpanel', { name: '工作区' });
  for (const image of images) {
    const name = 'fixture.' + image.extension;
    await page.getByRole('treeitem', { name, exact: true }).last().click();
    const preview = workspace.getByRole('img', { name, exact: true });
    await expect.poll(() => preview.evaluate((element: HTMLImageElement) => [element.naturalWidth, element.naturalHeight])).toEqual([image.width, image.height]);
    await workspace.getByRole('button', { name: '100%', exact: true }).click();
    await expect(preview).toHaveCSS('width', image.width + 'px');
    await workspace.getByRole('button', { name: '放大', exact: true }).click();
    await expect(preview).toHaveCSS('width', image.width * 1.25 + 'px');
    await workspace.getByRole('button', { name: '适应窗口', exact: true }).click();
  }
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'filesystem-images.png') });
  const whitespace = page.waitForResponse((response) => { const url = new URL(response.url()); return url.pathname.endsWith('/file') && url.searchParams.get('path') === ' fixture.png'; });
  // Accessible names trim outer whitespace; the literal leading-space name
  // sorts first. Check its raw label before clicking the otherwise identical name.
  const whitespaceRow = page.getByRole('treeitem', { name: 'fixture.png', exact: true }).first();
  expect(await whitespaceRow.locator('span.truncate').textContent()).toBe(' fixture.png');
  await whitespaceRow.click();
  expect((await (await whitespace).json()).data.path).toBe(' fixture.png');
  await expect(workspace.locator('span[title=" fixture.png"]')).toBeVisible();
  for (const image of images) {
    for (const prefix of ['damaged', 'empty']) {
      await selectWorkspaceFile(page, prefix + '.' + image.extension);
      await expect(workspace.getByText('无法显示此图片', { exact: true })).toBeVisible();
    }
  }
  await selectWorkspaceFile(page, 'oversized.png');
  await expect(workspace.getByText('此文件超过预览大小上限', { exact: true })).toBeVisible();
  expect(content).not.toContain('oversized.png');
  await selectWorkspaceFile(page, 'unsupported.tiff');
  await expect(workspace.getByText('此文件类型暂不支持预览', { exact: true })).toBeVisible();
  expect(content).not.toContain('unsupported.tiff');
});
