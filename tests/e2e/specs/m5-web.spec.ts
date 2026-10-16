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

  // Team lifecycle: archive pauses dispatch (messaging is refused with an
  // honest reason) and restore brings the team back.
  await page.getByRole('button', { name: /归档团队|Archive team/ }).first().click();
  await expect(page.getByText(/已归档|Archived/).first()).toBeVisible({ timeout: 15000 });
  await page.getByRole('button', { name: /恢复团队|Restore team/ }).first().click();
  await expect(page.getByText(/已归档|Archived/).first()).toBeHidden({ timeout: 15000 });
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
