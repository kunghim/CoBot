import { test, expect, type BrowserContext } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

async function seedTeam(context: BrowserContext, name: string) {
  await authorize(context);
  const csrf = (await context.cookies()).find((cookie) => cookie.name.startsWith('cobot_csrf_'))!.value;
  const directory = join(process.env.COBOT_E2E_ROOT!, name + '-' + Date.now());
  mkdirSync(directory, { recursive: true });
  setEngineMode('approve', directory);
  const headers = { 'X-CSRF-Token': csrf };
  const response = await context.request.post('/api/v1/teams', { headers, data: { requestId: randomUUID(), name, directory, initialRoleIds: ['backend-dev', 'qa-engineer'] } });
  expect(response.ok(), await response.text()).toBeTruthy();
  const data = (await response.json()).data;
  return { ...data, headers } as { team: { id: string }; bots: { id: string; roleId: string }[]; headers: Record<string, string> };
}

test('member scope and drafts survive switching between conversations', async ({ page, context }) => {
  const seeded = await seedTeam(context, 'scope-and-drafts');
  for (const [roleId, title] of [['leader', 'captain-only-task'], ['backend-dev', 'backend-only-task']]) {
    const owner = seeded.bots.find((bot) => bot.roleId === roleId)!;
    const response = await context.request.post(`/api/v1/teams/${seeded.team.id}/tasks`, { headers: seeded.headers, data: { requestId: randomUUID(), title, ownerId: owner.id } });
    expect(response.ok(), await response.text()).toBeTruthy();
  }
  await page.goto('/');
  const row = page.getByRole('complementary', { name: '团队列表' }).getByRole('listitem').filter({ hasText: 'scope-and-drafts' }).first();
  await row.getByRole('button', { name: '展开团队', exact: true }).click();
  const captain = row.getByRole('button', { name: /队长 队长$/ });
  const backend = row.getByRole('button', { name: /后端开发$/ });
  const qa = row.getByRole('button', { name: /测试工程师$/ });
  const panel = page.getByRole('complementary', { name: '工作面板' });
  await captain.click();
  await page.getByRole('textbox', { name: '给 队长 发消息……' }).fill('captain-draft');
  await expect(panel.getByText('captain-only-task', { exact: true })).toBeVisible();
  await expect(panel.getByText('backend-only-task', { exact: true })).toBeVisible();
  await backend.click();
  const composer = page.getByRole('textbox', { name: '给 后端开发 发消息……' });
  await expect(composer).toHaveValue('');
  await composer.fill('backend-draft');
  await expect(panel.getByText('backend-only-task', { exact: true })).toBeVisible();
  await expect(panel.getByText('captain-only-task', { exact: true })).toHaveCount(0);
  await qa.click();
  await expect(panel.getByText('暂无任务', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: '安全审批' })).toHaveCount(0);
  await captain.click();
  await expect(page.getByRole('textbox', { name: '给 队长 发消息……' })).toHaveValue('captain-draft');
  await backend.click();
  await expect(page.getByRole('textbox', { name: '给 后端开发 发消息……' })).toHaveValue('backend-draft');
  // User collapse must survive selection auto-reveal and query refreshes.
  await row.getByRole('button', { name: '收起团队', exact: true }).click();
  await expect(row.getByRole('button', { name: '展开团队', exact: true })).toBeVisible();
  await expect(backend).toHaveCount(0);
  await row.getByRole('button', { name: '展开团队', exact: true }).click();
  await expect(backend).toBeVisible();
});

test('task board can load records beyond its first page', async ({ page, context }) => {
  const seeded = await seedTeam(context, 'task-pagination');
  const owner = seeded.bots.find((bot) => bot.roleId === 'backend-dev')!;
  for (let i = 1; i <= 60; i++) {
    const response = await context.request.post(`/api/v1/teams/${seeded.team.id}/tasks`, { headers: seeded.headers, data: { requestId: randomUUID(), title: `page-task-${String(i).padStart(3, '0')}`, ownerId: owner.id } });
    expect(response.ok(), await response.text()).toBeTruthy();
  }
  await page.goto('/');
  const row = page.getByRole('complementary', { name: '团队列表' }).getByRole('listitem').filter({ hasText: 'task-pagination' }).first();
  await row.getByRole('button', { name: '展开团队', exact: true }).click();
  await row.getByRole('button', { name: /后端开发$/ }).click();
  const panel = page.getByRole('complementary', { name: '工作面板' });
  await expect(panel.getByText('page-task-060', { exact: true })).toBeVisible();
  await expect(panel.getByText('page-task-001', { exact: true })).toHaveCount(0);
  await panel.getByRole('button', { name: '继续加载', exact: true }).click();
  await expect(panel.getByText('page-task-001', { exact: true })).toBeVisible();
});
