import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { authorize, setEngineMode } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

// The five work-panel pages are all real in M5: tasks create, activity
// records, memories and collaboration show honest empty states, and the
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

  const leaders = page.getByRole('button', { name: /队长/ }).filter({ visible: true });
  await expect(leaders).toHaveCount(1, { timeout: 15000 });
  await leaders.first().click();

  // Tasks tab: create a task owned by the leader. The panel is the
  // complementary landmark; its header button shares the same accessible
  // name, so the role keeps the two apart.
  const panel = page.getByRole('complementary', { name: /工作面板|work panel/i });
  await expect(panel).toBeVisible();
  await panel.getByRole('tab', { name: /^任务$|^Tasks$/ }).click();
  await panel.getByRole('button', { name: /新建任务|new task/i }).click();
  await panel.getByLabel(/任务标题|title/i).fill('e2e任务');
  await panel.getByLabel(/负责人|owner/i).selectOption({ index: 1 });
  await panel.getByRole('button', { name: /^创建$|^Create$/ }).click();
  await expect(panel.getByText('e2e任务')).toBeVisible({ timeout: 15000 });

  // Activity tab: the task.created event is a durable fact.
  await panel.getByRole('tab', { name: /动态|Activity/ }).click();
  await expect(panel.getByText(/任务已创建|Task created/).first()).toBeVisible({ timeout: 15000 });

  // Memory and collaboration tabs show honest empty states, not fabricated
  // data and not "coming later".
  await panel.getByRole('tab', { name: /记忆|Memories/ }).click();
  await expect(panel.getByText(/暂无记忆|No memories yet/)).toBeVisible();
  await panel.getByRole('tab', { name: /协作/ }).click();
  await expect(panel.getByText(/暂无协作记录|No collaboration records/)).toBeVisible();

  // Team lifecycle: a team with an unfinished task cannot be archived; the
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

// I05: on a 320px screen the team list and work panel overlay the
// conversation and both stay reachable through their toggles.
test('narrow screen keeps both panels reachable', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 560 });
  await page.goto('/');

  // The primary rail stays; the two side panels overlay the conversation.
  await expect(page.getByRole('navigation').first()).toBeVisible();

  // Both panels start closed on narrow screens; the rail toggles open each
  // overlay, and each overlay's own header button closes it again.
  const listToggle = page.getByRole('button', { name: /团队列表|team list/i }).first();
  await expect(listToggle).toBeVisible();
  await listToggle.click();
  const listPanel = page.getByRole('complementary', { name: /团队列表|team list/i });
  await expect(listPanel).toBeVisible();
  await listPanel.getByRole('button', { name: /团队列表|team list/i }).first().click();
  await expect(listPanel).toBeHidden();
  await expect(listToggle).toBeVisible();

  const panelToggle = page.getByRole('button', { name: /工作面板|work panel/i }).first();
  await panelToggle.click();
  const narrowPanel = page.getByRole('complementary', { name: /工作面板|work panel/i });
  await expect(narrowPanel).toBeVisible();
  await narrowPanel.getByRole('button', { name: /工作面板|work panel/i }).first().click();
  await expect(narrowPanel).toBeHidden();
  await page.getByRole('button', { name: /工作面板|work panel/i }).first().click();
  await expect(narrowPanel).toBeVisible();
});
