import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, seedRequirement, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
  await context.addInitScript(() => localStorage.setItem('cobot.mode', 'work'));
});

// The work-panel pages are all real: the requirement board shows a route
// with its cards, memories show an honest empty state, and the
// team's archive/restore verbs really pause and resume dispatch.
test('work panel pages show real data and team lifecycle verbs work', async ({ page }) => {
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'm5-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  setEngineMode('reply', teamDir);

  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill('m5团队');
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  // A typed path validates on Enter through the same rules creation uses.
  await dialog.getByLabel(/工作目录|directory/i).press('Enter');
  await expect(dialog.getByText(/目录可用|directory usable/i)).toBeVisible({ timeout: 10000 });
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  await expect(page.getByText('m5团队').first()).toBeVisible({ timeout: 15000 });

  const leaders = page.getByRole('button', { name: /项目经理/ }).filter({ visible: true });
  await expect(leaders).toHaveCount(1, { timeout: 15000 });
  await leaders.first().click();

  // Tasks tab: a requirement with the route the user confirmed and the
  // card its engineer holds. Cards only come from the relay, so the spec
  // seeds one. The panel is the complementary landmark; its header button
  // shares the same accessible name, so the role keeps the two apart.
  const teams = (await (await page.request.get('/api/v1/teams')).json()).data.items as { team: { id: string; name: string }; bots: { id: string; isLeader: boolean }[] }[];
  const { team, bots } = teams.find((item) => item.team.name === 'm5团队')!;
  const leader = bots.find((bot) => bot.isLeader)!;
  seedRequirement(team.id, 'e2e需求', [
    { name: '用户确认', kind: 'confirm' },
    { name: 'e2e实现', kind: 'implement', ownerId: leader.id },
    { name: '验收汇报', kind: 'accept' },
  ], [{ title: 'e2e实现', ownerId: leader.id, stageIndex: 1 }]);
  await page.reload();
  // The pages live in the info card over the conversation now: the header
  // button opens it, and it shows as a labelled region.
  await page.getByRole('button', { name: /展开信息面板|open the info panel/i }).click();
  const panel = page.getByRole('region', { name: /信息面板|info panel/i });
  await expect(panel).toBeVisible();
  await panel.getByRole('tab', { name: /^任务$|^Tasks$/ }).click();
  await panel.getByRole('button', { name: /e2e需求/ }).click();
  await expect(panel.getByRole('list').getByText(/e2e实现 · 实现/)).toBeVisible({ timeout: 15000 });
  await expect(panel.locator('li[id^="task-"]').filter({ hasText: 'e2e实现' })).toBeVisible();
  await expect(panel.getByRole('button', { name: '取消需求' })).toBeVisible();

  // The memory tab shows an honest empty state; member-to-member messages
  // live in conversations and activity, so there is no collaboration tab.
  await panel.getByRole('tab', { name: /记忆|Memories/ }).click();
  await expect(panel.getByText(/暂无记忆|No memories yet/)).toBeVisible();
  await expect(panel.getByRole('tab', { name: /协作/ })).toHaveCount(0);

  // Team lifecycle: a team with an open card cannot be archived; the
  // refusal names what is left.
  // Archiving asks first; the refusal shows inside the dialog.
  await page.getByRole('button', { name: /团队操作|Actions for/ }).first().click();
  await page.getByRole('menuitem', { name: /归档团队|Archive team/ }).click();
  const confirmArchive = page.getByRole('alertdialog');
  await confirmArchive.getByRole('button', { name: /归档团队|Archive team/ }).click();
  await expect(confirmArchive.getByRole('alert').filter({ hasText: /暂不能归档|cannot be archived/ })).toBeVisible({ timeout: 15000 });
});

// Archiving an idle team moves it from the sidebar to Settings › Archived
// teams, where it can be found and restored back to the sidebar.
test('an idle team archives to settings and restores', async ({ page }) => {
  const label = '归档测试' + Date.now();
  const teamDir = join(process.env.COBOT_E2E_ROOT!, 'm5-archive-' + Date.now());
  mkdirSync(teamDir, { recursive: true });
  setEngineMode('reply', teamDir);

  await page.goto('/');
  await page.getByRole('button', { name: /新建团队|create team/i }).first().click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel(/团队名称|team name/i).fill(label);
  await dialog.getByLabel(/工作目录|directory/i).fill(teamDir);
  // Only the project manager comes by default; the rest is picked.
  await dialog.getByRole('checkbox', { name: /测试工程师|QA engineer/ }).check();
  await dialog.getByRole('button', { name: /创建|create/i }).click();
  const sidebar = page.getByRole('complementary', { name: /团队列表|team list/i });
  const row = sidebar.locator('li').filter({ hasText: label }).first();
  await expect(row).toBeVisible({ timeout: 15000 });

  // Members are deleted from the same menu: pick one, then confirm.
  const expand = row.locator('button[aria-expanded="false"]:not([aria-haspopup])');
  if ((await expand.count()) > 0) await expand.click();
  await expect(row.getByRole('button', { name: /测试工程师|QA engineer/ })).toBeVisible({ timeout: 15000 });
  await row.getByRole('button', { name: /团队操作|Actions for/ }).click();
  await page.getByRole('menuitem', { name: /删除成员|Delete member/ }).click();
  const deleteMember = page.getByRole('alertdialog');
  await deleteMember.getByRole('radio', { name: /测试工程师|QA engineer/ }).check();
  await deleteMember.getByRole('button', { name: /删除成员|Delete member/ }).click();
  await expect(row.getByRole('button', { name: /测试工程师|QA engineer/ })).toHaveCount(0, { timeout: 15000 });

  // Editing renames the team and adds members back; existing members and the
  // folder stay as they are.
  await row.getByRole('button', { name: /团队操作|Actions for/ }).click();
  await page.getByRole('menuitem', { name: /编辑团队|Edit team/ }).click();
  const edit = page.getByRole('dialog');
  await expect(edit.getByText(teamDir)).toBeVisible();
  await edit.getByLabel(/团队名称|Team name/).fill(label + '-改');
  await edit.getByRole('checkbox', { name: /测试工程师|QA engineer/ }).click();
  await edit.getByRole('button', { name: /^保存$|^Save$/ }).click();
  await expect(sidebar.getByText(label + '-改')).toBeVisible({ timeout: 15000 });
  await expect(row.getByRole('button', { name: /测试工程师|QA engineer/ })).toBeVisible({ timeout: 15000 });

  await row.getByRole('button', { name: /团队操作|Actions for/ }).click();
  await page.getByRole('menuitem', { name: /归档团队|Archive team/ }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: /归档团队|Archive team/ }).click();
  await expect(sidebar.getByText(label)).toHaveCount(0, { timeout: 15000 });

  await page.goto('/settings/archived');
  await page.getByRole('searchbox').fill(label);
  const archivedRow = page.locator('li').filter({ hasText: label });
  await expect(archivedRow).toBeVisible();
  await archivedRow.getByRole('button', { name: /恢复团队|Restore team/ }).click();
  await expect(page.getByText(/已恢复|is restored/)).toBeVisible({ timeout: 15000 });

  await page.goto('/');
  await expect(page.getByRole('complementary', { name: /团队列表|team list/i }).getByText(label)).toBeVisible({ timeout: 15000 });
});

// On a narrow screen the team list stays in the layout, beside the
// page and never over it: it folds away through its toggle and nothing
// modal opens. The workspace yields to the conversation on narrow windows
// by never taking more than its share.
test('narrow screen keeps the team list beside the page', async ({ page }) => {
  await page.setViewportSize({ width: 640, height: 560 });
  await page.goto('/');

  await expect(page.getByRole('navigation').first()).toBeVisible();
  const listPanel = page.getByRole('complementary', { name: /团队列表|team list/i });

  // A narrow window starts with the list open, beside the page.
  await expect(listPanel).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);

  // The toggle folds and reopens it.
  await page.getByRole('button', { name: /收起团队列表|collapse team list/i }).click();
  await expect(listPanel).toBeHidden();
  await page.getByRole('button', { name: /展开团队列表|expand team list/i }).click();
  await expect(listPanel).toBeVisible();
});
