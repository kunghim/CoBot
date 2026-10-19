import { test, expect, type BrowserContext } from '@playwright/test';
import { randomUUID } from 'node:crypto';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, seedRequirement, setEngineMode } from './helpers';

async function seedTeam(context: BrowserContext, name: string) {
  await authorize(context);
  const csrf = (await context.cookies()).find((cookie) => cookie.name.startsWith('cobot_csrf_'))!.value;
  const directory = join(process.env.COBOT_E2E_ROOT!, name + '-' + Date.now());
  mkdirSync(directory, { recursive: true });
  setEngineMode('approve', directory);
  const headers = { 'X-CSRF-Token': csrf };
  const response = await context.request.post('/api/v1/teams', { headers, data: { requestId: randomUUID(), name, directory, initialRoleIds: ['fullstack-engineer', 'qa-engineer'] } });
  expect(response.ok(), await response.text()).toBeTruthy();
  const data = (await response.json()).data;
  return { ...data, headers } as { team: { id: string }; bots: { id: string; roleId: string }[]; headers: Record<string, string> };
}

test('member scope and drafts survive switching between conversations', async ({ page, context }) => {
  const seeded = await seedTeam(context, 'scope-and-drafts');
  // One requirement whose card the project manager holds, one the engineer holds.
  for (const [roleId, title, kind] of [['leader', 'captain-only-task', 'accept'], ['fullstack-engineer', 'backend-only-task', 'implement']]) {
    const owner = seeded.bots.find((bot) => bot.roleId === roleId)!;
    seedRequirement(seeded.team.id, title, [{ name: '用户确认', kind: 'confirm' }, { name: title, kind, ownerId: owner.id }], [{ title, ownerId: owner.id, stageIndex: 1 }]);
  }
  await page.goto('/');
  const row = page.getByRole('complementary', { name: '团队列表' }).getByRole('listitem').filter({ hasText: 'scope-and-drafts' }).first();
  await row.locator('button[aria-expanded="false"]:not([aria-haspopup])').first().click();
  const captain = row.getByRole('button', { name: /项目经理 项目经理$/ });
  const backend = row.getByRole('button', { name: /全栈工程师$/ });
  const qa = row.getByRole('button', { name: /测试工程师$/ });
  const panel = page.getByRole('complementary', { name: '工作面板' });
  await captain.click();
  await page.getByRole('textbox', { name: '给 项目经理 发消息……' }).fill('captain-draft');
  await expect(panel.getByText('captain-only-task', { exact: true })).toBeVisible();
  await expect(panel.getByText('backend-only-task', { exact: true })).toBeVisible();
  await backend.click();
  const composer = page.getByRole('textbox', { name: '给 全栈工程师 发消息……' });
  await expect(composer).toHaveValue('');
  await composer.fill('backend-draft');
  await expect(panel.getByText('backend-only-task', { exact: true })).toBeVisible();
  await expect(panel.getByText('captain-only-task', { exact: true })).toHaveCount(0);
  await qa.click();
  await expect(panel.getByText('暂无需求', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: '安全审批' })).toHaveCount(0);
  await captain.click();
  await expect(page.getByRole('textbox', { name: '给 项目经理 发消息……' })).toHaveValue('captain-draft');
  await backend.click();
  await expect(page.getByRole('textbox', { name: '给 全栈工程师 发消息……' })).toHaveValue('backend-draft');
  // User collapse must survive selection auto-reveal and query refreshes.
  await row.locator('button[aria-expanded="true"]:not([aria-haspopup])').first().click();
  await expect(row.locator('button[aria-expanded="false"]:not([aria-haspopup])').first()).toBeVisible();
  await expect(backend).toHaveCount(0);
  await row.locator('button[aria-expanded="false"]:not([aria-haspopup])').first().click();
  await expect(backend).toBeVisible();
});

test('task board can load cards beyond its first page', async ({ page, context }) => {
  const seeded = await seedTeam(context, 'task-pagination');
  const owner = seeded.bots.find((bot) => bot.roleId === 'fullstack-engineer')!;
  const cards = Array.from({ length: 60 }, (_, i) => ({ title: `page-task-${String(i + 1).padStart(3, '0')}`, ownerId: owner.id, stageIndex: 1, status: 'done' as const }));
  seedRequirement(seeded.team.id, 'page-requirement', [{ name: '用户确认', kind: 'confirm' }, { name: '实现', kind: 'implement', ownerId: owner.id }], cards);
  await page.goto('/');
  const row = page.getByRole('complementary', { name: '团队列表' }).getByRole('listitem').filter({ hasText: 'task-pagination' }).first();
  await row.locator('button[aria-expanded="false"]:not([aria-haspopup])').first().click();
  await row.getByRole('button', { name: /全栈工程师$/ }).click();
  const panel = page.getByRole('complementary', { name: '工作面板' });
  await panel.getByRole('button', { name: /page-requirement/ }).click();
  await expect(panel.getByText('page-task-060', { exact: true })).toBeVisible();
  await expect(panel.getByText('page-task-001', { exact: true })).toHaveCount(0);
  await panel.getByRole('button', { name: '继续加载', exact: true }).click();
  await expect(panel.getByText('page-task-001', { exact: true })).toBeVisible();
});
