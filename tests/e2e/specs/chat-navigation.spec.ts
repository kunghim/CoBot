import { expect, test, type Page } from '@playwright/test';
import { join } from 'node:path';
import { authorize } from './helpers';

test.beforeEach(async ({ context }) => { await authorize(context); });

async function createChat(page: Page, title: string) {
  await page.goto('/chat/new');
  await page.getByRole('textbox', { name: '发送消息' }).fill(title);
  await page.getByRole('button', { name: '发送', exact: true }).click();
  await expect(page).toHaveURL(/\/chat\/(?!new$)[a-z0-9-]+$/);
  await expect(page.getByText('收到：' + title, { exact: true })).toBeVisible();
  return page.url().split('/').pop()!;
}

test('chat sections, pinned state, nested menu keyboard and manual order persist', async ({ page }) => {
  const title = '侧栏组织-' + Date.now();
  const first = await createChat(page, title);
  const row = () => page.locator(`[data-chat-id="${first}"]`);
  await row().getByRole('button', { name: '聊天操作' }).click();
  await page.getByRole('menuitem', { name: '置顶', exact: true }).click();
  await expect(page.getByRole('region', { name: '置顶', exact: true }).locator(`[data-chat-id="${first}"]`)).toBeVisible();
  expect(await row().count()).toBe(1);

  await page.getByRole('button', { name: '整理聊天列表' }).click();
  const grouping = page.getByRole('menuitem', { name: '显示方式', exact: true });
  await grouping.focus(); await grouping.press('ArrowRight');
  await expect(page.getByRole('menuitemradio', { name: '按分区显示' })).toBeVisible();
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'chat-organize-menu.png') });
  await page.getByRole('menuitemradio', { name: '按分区显示' }).press('Escape');
  await expect(grouping).toBeFocused();
  await grouping.press('Escape');
  await expect(page.getByRole('button', { name: '整理聊天列表' })).toBeFocused();

  await page.getByRole('button', { name: '整理聊天列表' }).click();
  await page.getByRole('menuitem', { name: '新建分区' }).click();
  await page.getByRole('textbox', { name: '分区名称' }).fill(title + '-分区');
  await page.getByRole('alertdialog').getByRole('button', { name: '保存', exact: true }).click();
  await expect(page.getByRole('button', { name: title + '-分区', exact: true })).toBeVisible();

  await row().getByRole('button', { name: '聊天操作' }).click();
  await page.getByRole('menuitem', { name: '移至分区' }).press('ArrowRight');
  await page.getByRole('menuitemradio', { name: title + '-分区', exact: true }).click();
  await row().getByRole('button', { name: '聊天操作' }).click();
  await page.getByRole('menuitem', { name: '取消置顶' }).click();
  await expect(page.getByRole('region', { name: title + '-分区', exact: true }).locator(`[data-chat-id="${first}"]`)).toBeVisible();

  await page.getByRole('button', { name: '整理聊天列表' }).click();
  await page.getByRole('menuitem', { name: '聊天排序方式' }).press('ArrowRight');
  await page.getByRole('menuitemradio', { name: '手动排序' }).click();
  await page.reload();
  await row().getByRole('button', { name: '聊天操作' }).click();
  await expect(page.getByRole('menuitem', { name: '上移', exact: true })).toBeVisible();
  await page.keyboard.press('Escape');

  await page.getByRole('button', { name: '整理聊天列表' }).click();
  await page.getByRole('menuitem', { name: '显示方式', exact: true }).press('ArrowRight');
  await page.getByRole('menuitemradio', { name: '合并显示' }).click();
  await expect(row()).toBeVisible();
  await page.getByRole('button', { name: '整理聊天列表' }).click();
  await page.getByRole('menuitem', { name: '显示方式', exact: true }).press('ArrowRight');
  await page.getByRole('menuitemradio', { name: '按分区显示' }).click();
  await page.getByRole('button', { name: '分区操作：' + title + '-分区', exact: true }).click();
  await page.getByRole('menuitem', { name: '删除分区', exact: true }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: '删除', exact: true }).click();
  await expect(row()).toBeVisible();
  await expect(page.getByRole('button', { name: title + '-分区', exact: true })).toHaveCount(0);
});

test('unified archive switches chat and team without carrying selection; restore continues the chat', async ({ page }) => {
  const title = '归档恢复-' + Date.now();
  const id = await createChat(page, title);
  await page.locator('header').getByRole('button', { name: '聊天操作' }).click();
  await page.getByRole('menuitem', { name: '归档聊天', exact: true }).click();
  await expect(page).toHaveURL(/\/chat\/new$/);
  await expect(page.locator(`[data-chat-id="${id}"]`)).toHaveCount(0);

  await page.getByRole('button', { name: '整理聊天列表' }).click();
  await page.getByRole('menuitem', { name: '已归档聊天', exact: true }).click();
  await expect(page).toHaveURL(/\/settings\/archived\?type=chat$/);
  await expect(page.getByRole('heading', { name: '已归档', exact: true })).toBeVisible();
  const tabs = page.getByRole('radiogroup', { name: '归档类型' });
  await expect(tabs.getByRole('radio', { name: '聊天', exact: true })).toHaveAttribute('aria-checked', 'true');
  await page.getByRole('searchbox').fill(title);
  let row = page.locator('li').filter({ hasText: title });
  await row.getByRole('checkbox').check();
  await expect(page.getByRole('button', { name: '删除所选（1）' })).toBeVisible();
  await tabs.getByRole('radio', { name: '团队', exact: true }).click();
  await expect(page.getByRole('button', { name: '删除所选（1）' })).toHaveCount(0);
  await tabs.getByRole('radio', { name: '聊天', exact: true }).click();
  await expect(page.getByRole('button', { name: '删除所选（1）' })).toHaveCount(0);
  await page.getByRole('searchbox').fill(title);
  row = page.locator('li').filter({ hasText: title });
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'unified-archive-chat.png') });
  await row.getByRole('button', { name: '恢复聊天', exact: true }).click();
  await expect(row).toHaveCount(0);
  await page.goto(`/chat/${id}`);
  await expect(page.getByText('收到：' + title, { exact: true })).toBeVisible();
  await page.getByRole('textbox', { name: '发送消息' }).fill('恢复后继续');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  await expect(page.getByText('收到：恢复后继续', { exact: true })).toBeVisible();
  await expect(page.getByRole('radio', { name: '团队', exact: true })).toBeVisible();
});

test('manual drag and keyboard order agree; nested choices stay inside a narrow window', async ({ page }) => {
  const prefix = '手动顺序-' + Date.now();
  const ids: string[] = [];
  for (let i = 0; i < 3; i++) ids.push(await createChat(page, prefix + '-' + i));
  await page.getByRole('button', { name: '整理聊天列表' }).click();
  await page.getByRole('menuitem', { name: '聊天排序方式' }).press('ArrowRight');
  await page.getByRole('menuitemradio', { name: '手动排序' }).click();
  await page.getByRole('button', { name: '整理聊天列表' }).click();
  await page.getByRole('menuitem', { name: '显示方式', exact: true }).press('ArrowRight');
  await page.getByRole('menuitemradio', { name: '合并显示' }).click();
  const order = () => page.locator('[data-chat-id]').evaluateAll((rows, targets) => rows.map((row) => row.getAttribute('data-chat-id')).filter((id) => targets.includes(id!)), ids);
  await expect.poll(order).toEqual(ids);
  await page.locator(`[data-chat-id="${ids[2]}"]`).dragTo(page.locator(`[data-chat-id="${ids[0]}"]`));
  await expect.poll(order).toEqual([ids[2], ids[0], ids[1]]);
  await page.locator(`[data-chat-id="${ids[2]}"]`).getByRole('button', { name: '聊天操作' }).click();
  await page.getByRole('menuitem', { name: '下移', exact: true }).click();
  await expect.poll(order).toEqual([ids[0], ids[2], ids[1]]);
  await page.reload(); await expect.poll(order).toEqual([ids[0], ids[2], ids[1]]);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: '整理聊天列表' }).click();
  await page.getByRole('menuitem', { name: '显示方式', exact: true }).press('ArrowRight');
  const bounds = await page.getByRole('menu', { name: '显示方式', exact: true }).boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0); expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(390);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'chat-menu-narrow.png') });
});

test('archive bulk delete reports partial failure and keeps the failed chat', async ({ page }) => {
  const prefix = '批量归档-' + Date.now(); const ids: string[] = [];
  for (let i = 0; i < 2; i++) {
    ids.push(await createChat(page, prefix + '-' + i));
    await page.locator('header').getByRole('button', { name: '聊天操作' }).click();
    await page.getByRole('menuitem', { name: '归档聊天', exact: true }).click();
    await expect(page).toHaveURL(/\/chat\/new$/);
  }
  await page.goto('/settings/archived?type=chat'); await page.getByRole('searchbox').fill(prefix);
  await expect(page.locator('li').filter({ hasText: prefix })).toHaveCount(2);
  await page.getByRole('checkbox', { name: '全选', exact: true }).check();
  await page.route(`**/api/v1/chats/${ids[1]}`, async (route) => {
    if (route.request().method() === 'DELETE') await route.fulfill({ status: 409, contentType: 'application/json', body: JSON.stringify({ error: { code: 'conflict', messageKey: 'error.chat.revision_conflict' }, requestId: 'test-conflict' }) });
    else await route.continue();
  });
  await page.getByRole('button', { name: '删除所选（2）' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: '删除聊天', exact: true }).click();
  await expect(page.getByRole('main').getByRole('alert')).toContainText('1 段聊天删除失败');
  await expect(page.locator('li').filter({ hasText: prefix + '-0' })).toHaveCount(0);
  await expect(page.locator('li').filter({ hasText: prefix + '-1' })).toBeVisible();
  await expect(page.getByRole('button', { name: '删除所选（1）' })).toBeVisible();
});
