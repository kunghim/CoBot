import { test, expect } from '@playwright/test';
import { authorize, setEngineMode } from './helpers';
import { join } from 'node:path';

test.beforeEach(async ({ context }) => { await authorize(context); });

test('projectless chat persists, switches modes, renames and deletes', async ({ page }) => {
  await page.goto('/chat/new');
  await expect(page.getByTestId('chat-welcome')).toBeVisible();
  await page.getByRole('textbox', { name: '发送消息' }).fill('聊天首版验收');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  await expect(page).toHaveURL(/\/chat\/(?!new$)[a-z0-9-]+$/);
  const url = page.url();
  await expect(page.getByText('收到：聊天首版验收', { exact: true })).toBeVisible({ timeout: 20_000 });
  await page.getByRole('textbox', { name: '发送消息' }).fill('保留草稿');
  await page.getByRole('radio', { name: '团队', exact: true }).click();
  await expect(page.getByRole('button', { name: /新建团队/ }).first()).toBeVisible();
  await page.getByRole('radio', { name: '团队', exact: true }).press('ArrowRight');
  await expect(page).toHaveURL(url);
  await expect(page.getByRole('textbox', { name: '发送消息' })).toHaveValue('保留草稿');
  await page.reload();
  await expect(page.getByText('收到：聊天首版验收', { exact: true })).toBeVisible();
  await page.locator('header').getByRole('button', { name: '聊天操作' }).click();
  await page.getByRole('menuitem', { name: '重命名' }).click();
  await page.getByRole('textbox', { name: '聊天名称' }).fill('重命名后的聊天');
  await page.getByRole('button', { name: '保存', exact: true }).click();
  await expect(page.getByRole('heading', { name: '重命名后的聊天' })).toBeVisible();
  await page.locator('header').getByRole('button', { name: '聊天操作' }).click();
  await page.getByRole('menuitem', { name: '删除聊天' }).click();
  await page.getByRole('alertdialog').getByRole('button', { name: '删除聊天' }).click();
  await expect(page).toHaveURL(/\/chat\/new$/);
  await expect(page.getByRole('button', { name: '重命名后的聊天', exact: true })).toHaveCount(0);
});

test('chat handles native approval and preserves drafts while running', async ({ page }) => {
  await page.goto('/chat/new');
  const first = page.waitForResponse((response) => response.url().endsWith('/api/v1/chats') && response.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('先建立聊天');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await first).json()).data;
  await expect(page.getByText('收到：先建立聊天', { exact: true })).toBeVisible({ timeout: 20_000 });
  setEngineMode('approve', created.chat.workspace);
  try {
    await page.getByRole('textbox', { name: '发送消息' }).fill('需要批准的操作');
    await page.getByRole('button', { name: '发送', exact: true }).click();
    await expect(page.getByRole('heading', { name: '需要你的批准' })).toBeVisible({ timeout: 20_000 });
    await page.getByRole('textbox', { name: '发送消息' }).fill('运行时的草稿');
    await expect(page.getByRole('button', { name: '停止生成', exact: true })).toBeVisible();
    await page.getByRole('button', { name: '允许', exact: true }).click();
    await expect(page.getByText(/收到：需要批准的操作/)).toBeVisible({ timeout: 20_000 });
    await expect(page.getByRole('textbox', { name: '发送消息' })).toHaveValue('运行时的草稿');
  } finally { setEngineMode('reply', created.chat.workspace); }
});

test('chat layout stays usable in dark and narrow windows', async ({ page }) => {
  await page.goto('/chat/new');
  await expect(page.getByTestId('chat-welcome')).toBeVisible();
  const output = process.env.COBOT_E2E_ROOT!;
  await page.screenshot({ path: join(output, 'chat-light.png'), animations: 'disabled' });
  // Theme classes are applied by the product; this screenshot isolates the
  // chat layout from the profile setting, which other specs share.
  await page.locator('html').evaluate((element) => { element.dataset.mode = 'dark'; element.style.colorScheme = 'dark'; });
  await expect(page.locator('html')).toHaveAttribute('data-mode', 'dark');
  await page.screenshot({ path: join(output, 'chat-dark.png'), animations: 'disabled' });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('textbox', { name: '发送消息' })).toBeVisible();
  expect(await page.locator('body').evaluate((element) => element.scrollWidth <= window.innerWidth)).toBe(true);
  await page.getByRole('button', { name: '收起聊天记录', exact: true }).click();
  await expect(page.getByRole('complementary', { name: '聊天记录' })).toHaveCount(0);
  await expect(page.getByRole('radiogroup', { name: '使用模式' })).toBeHidden();
  await page.screenshot({ path: join(output, 'chat-narrow.png'), animations: 'disabled' });
  await page.getByRole('button', { name: '展开聊天记录', exact: true }).click();
  await expect(page.getByRole('radio', { name: '团队', exact: true })).toBeVisible();
});

test('chat has one greeting, a compact four-slot title bar and safe keyboard input', async ({ page }) => {
  await page.clock.setFixedTime(new Date(2026, 9, 24, 8, 30));
  await page.goto('/chat/new');
  const welcome = page.getByTestId('chat-welcome');
  await expect(welcome).toHaveText('早上好，今天想聊些什么？');
  await expect(page.getByRole('heading')).toHaveCount(1);
  await expect(page.getByTestId('chat-composer')).toBeVisible();
  const header = page.getByTestId('app-mode-header');
  const brand = await header.getByText('CoBot', { exact: true }).boundingBox();
  const mode = await header.getByRole('radio', { name: '团队', exact: true }).boundingBox();
  expect(mode!.x).toBeGreaterThan(brand!.x + brand!.width);
  const logo = await header.getByTestId('chrome-logo').boundingBox();
  const toggle = await header.getByRole('button', { name: '收起聊天记录', exact: true }).boundingBox();
  expect(brand!.x).toBeGreaterThanOrEqual(logo!.x + logo!.width);
  expect(toggle!.x).toBeGreaterThanOrEqual(mode!.x + mode!.width);
  expect((await page.getByTestId('app-title-bar').boundingBox())!.height).toBe(40);
  expect((await page.getByRole('main').boundingBox())!.y).toBe(48);
  expect((await header.getByRole('radiogroup', { name: '使用模式' }).boundingBox())!.height).toBe(24);
  expect(mode!.height).toBe(20);
  expect(mode!.width).toBe(24);
  await expect(header.getByRole('radio', { name: '团队', exact: true }).getByTitle('团队')).toBeVisible();
  const sidebar = page.getByRole('complementary', { name: '聊天记录' });
  const sidebarBounds = await sidebar.boundingBox();
  expect(toggle!.x).toBeCloseTo(sidebarBounds!.x + sidebarBounds!.width, 0);
  await header.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'chat-titlebar.png'), animations: 'disabled' });
  const create = await sidebar.getByRole('button', { name: '新建聊天', exact: true }).boundingBox();
  expect((await sidebar.getByText('聊天记录', { exact: true }).boundingBox())!.y).toBeGreaterThan(create!.y + create!.height);
  expect(toggle!.height).toBe(36);
  expect(toggle!.width).toBe(28);
  expect((await header.getByRole('button', { name: '收起聊天记录', exact: true }).locator('svg').boundingBox())!.height).toBe(20);
  expect((await header.getByRole('button', { name: '收起聊天记录', exact: true }).locator('svg').boundingBox())!.width).toBe(16);
  const toggleX = toggle!.x;
  await header.getByRole('button', { name: '收起聊天记录', exact: true }).click();
  expect((await header.getByRole('button', { name: '展开聊天记录', exact: true }).boundingBox())!.x).toBeLessThan(toggleX);
  await expect(header.getByTestId('chrome-wordmark')).toBeHidden();
  await expect(header.getByRole('radiogroup', { name: '使用模式' })).toBeHidden();
  expect((await header.getByRole('button', { name: '展开聊天记录', exact: true }).boundingBox())!.height).toBe(36);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'chat-collapsed.png'), animations: 'disabled' });
  await header.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'chat-titlebar-collapsed.png'), animations: 'disabled' });
  await header.getByRole('button', { name: '展开聊天记录', exact: true }).click();
  await expect(header.getByRole('radio', { name: '聊天', exact: true })).toHaveAttribute('aria-checked', 'true');
  const input = page.getByRole('textbox', { name: '发送消息', exact: true });
  await input.fill('输入法确认');
  await page.clock.setFixedTime(new Date(2026, 9, 24, 15));
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(welcome).toHaveText('早上好，今天想聊些什么？');
  await input.dispatchEvent('keydown', { key: 'Enter', isComposing: true, keyCode: 229 });
  await expect(page).toHaveURL(/\/chat\/new$/);
  await input.press('Shift+Enter');
  await expect(input).toHaveValue('输入法确认\n');
  await input.fill('');
  await expect(welcome).toHaveText('下午好，想聊些什么？');
  await input.fill('按回车发送');
  await input.press('Enter');
  await expect(page).toHaveURL(/\/chat\/(?!new$)[a-z0-9-]+$/);
  await expect(welcome).toHaveCount(0);
  await expect(page.getByTestId('chat-composer')).toBeVisible();
});

test('desktop uses a single compact title bar and protects native controls from mode double clicks', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('cobot.mode', 'work');
    const target = window as unknown as { go: object; runtime: object; chromeMaximizeCalls: number };
    target.go = {};
    target.chromeMaximizeCalls = 0;
    target.runtime = {
      Environment: async () => ({ platform: 'windows' }),
      WindowIsMaximised: async () => false,
      WindowMinimise: () => undefined,
      WindowToggleMaximise: () => { target.chromeMaximizeCalls += 1; },
      Quit: () => undefined,
    };
  });
  await page.goto('/');
  const bar = page.getByTestId('app-title-bar');
  await expect(page.getByRole('button', { name: '最小化', exact: true })).toBeVisible();
  await expect(bar).toHaveCount(1);
  await expect(page.getByTestId('app-mode-header')).toHaveCount(1);
  expect((await bar.boundingBox())!.height).toBe(40);
  expect((await page.getByRole('main').boundingBox())!.y).toBe(48);
  const sidebar = page.getByRole('complementary', { name: '团队列表' });
  const create = await sidebar.getByRole('button', { name: '新建团队', exact: true }).boundingBox();
  expect((await sidebar.getByText('团队列表', { exact: true }).boundingBox())!.y).toBeGreaterThan(create!.y + create!.height);
  await page.getByRole('radio', { name: '团队', exact: true }).dblclick();
  expect(await page.evaluate(() => (window as unknown as { chromeMaximizeCalls: number }).chromeMaximizeCalls)).toBe(0);
  await bar.dblclick({ position: { x: 650, y: 20 } });
  expect(await page.evaluate(() => (window as unknown as { chromeMaximizeCalls: number }).chromeMaximizeCalls)).toBe(1);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'native-work-layout.png'), animations: 'disabled' });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('radio', { name: '团队', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: '关闭', exact: true })).toBeVisible();
  expect(await page.locator('body').evaluate((element) => element.scrollWidth <= window.innerWidth)).toBe(true);
});


test('sidebar toggle clears its background immediately when moving away from the pointer', async ({ page }) => {
  await page.goto('/chat/new');
  await expect(page.getByTestId('chat-welcome')).toBeVisible();
  for (const label of ['收起聊天记录', '展开聊天记录']) {
    const button = page.getByRole('button', { name: label, exact: true });
    const before = (await button.boundingBox())!;
    await page.mouse.move(before.x + before.width / 2, before.y + before.height / 2);
    await expect(button).not.toHaveCSS('background-color', 'rgba(0, 0, 0, 0)');
    await page.evaluate(() => {
      const button = document.querySelector<HTMLButtonElement>('[data-testid="app-mode-header"] button[aria-expanded]')!;
      const samples: Array<{ x: number; background: string; hover: boolean; backgroundAnimations: number }> = [];
      (window as unknown as { sidebarSamples: typeof samples }).sidebarSamples = samples;
      const start = performance.now();
      const sample = () => {
        samples.push({ x: button.getBoundingClientRect().x, background: getComputedStyle(button).backgroundColor, hover: button.matches(':hover'), backgroundAnimations: button.getAnimations().filter((animation) => (animation as CSSTransition).transitionProperty === 'background-color').length });
        if (performance.now() - start < 350) requestAnimationFrame(sample);
      };
      requestAnimationFrame(sample);
    });
    await page.mouse.down();
    await page.waitForTimeout(60);
    await page.mouse.up();
    await expect(button).toHaveCount(0);
    // Sample the whole former 150ms fade after the layout has moved the button.
    await page.waitForTimeout(380);
    const samples = await page.evaluate(() => (window as unknown as { sidebarSamples: Array<{ x: number; background: string; hover: boolean; backgroundAnimations: number }> }).sidebarSamples);
    const moved = samples.filter((sample) => sample.x !== before.x && !sample.hover);
    expect(moved.length).toBeGreaterThan(3);
    expect(moved.every((sample) => sample.background === 'rgba(0, 0, 0, 0)' && sample.backgroundAnimations === 0)).toBe(true);
  }
});
