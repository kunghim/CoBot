import { test, expect } from '@playwright/test';
import { authorize, setEngineMode } from './helpers';
import { join } from 'node:path';

test.beforeEach(async ({ context }) => { await authorize(context); });

test('header search finds a chat by message content and jumps into it', async ({ page }) => {
  await page.goto('/chat/new');
  const first = page.waitForResponse((response) => response.url().endsWith('/api/v1/chats') && response.request().method() === 'POST');
  await page.getByRole('textbox', { name: '发送消息' }).fill('搜索标记-蓝鲸巡检');
  await page.getByRole('button', { name: '发送', exact: true }).click();
  const created = (await (await first).json()).data;
  await expect(page.getByText(/收到：搜索标记-蓝鲸巡检/)).toBeVisible({ timeout: 20_000 });
  await page.goto('/chat/new');
  const search = page.getByRole('combobox', { name: '搜索会话' });
  await search.fill('蓝鲸巡检');
  const hit = page.getByRole('option', { name: /蓝鲸巡检/ });
  await expect(hit).toBeVisible({ timeout: 10_000 });
  await hit.click();
  await expect(page).toHaveURL(new RegExp(`/chat/${created.chat.id}$`));
  // A phrase no conversation carries closes with an explicit empty state.
  await search.fill('蓝鲸巡检-不存在');
  await expect(page.getByText('没有匹配的会话')).toBeVisible({ timeout: 10_000 });
});

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

test('chat has one greeting, controls aligned with the sidebar and conversation and safe keyboard input', async ({ page }) => {
  await page.clock.setFixedTime(new Date(2026, 9, 24, 8, 30));
  await page.goto('/chat/new');
  const welcome = page.getByTestId('chat-welcome');
  await expect(welcome).toHaveText('早上好，今天想聊些什么？');
  await expect(page.getByRole('heading')).toHaveCount(1);
  await expect(page.getByTestId('chat-composer')).toBeVisible();
  const header = page.getByTestId('app-mode-header');
  const bar = page.getByTestId('app-title-bar');
  await expect(header.getByText('CoBot', { exact: true })).toHaveCount(0);
  await expect(header.locator('img')).toHaveCount(0);
  const mode = await header.getByRole('radio', { name: '团队', exact: true }).boundingBox();
  const logo = await page.getByTestId('rail-logo').boundingBox();
  const home = await page.getByRole('navigation', { name: 'CoBot', exact: true }).getByRole('button', { name: '新建聊天', exact: true }).boundingBox();
  expect(logo!.y + logo!.height).toBeLessThanOrEqual(home!.y);
  const toggle = await bar.getByRole('button', { name: '收起聊天记录', exact: true }).boundingBox();
  expect(toggle!.x).toBeGreaterThanOrEqual(mode!.x + mode!.width);
  expect((await bar.boundingBox())!.height).toBe(40);
  // The columns reach the top of the window; the conversation head is the
  // first row of main.
  expect((await page.getByRole('main').boundingBox())!.y).toBe(0);
  expect((await header.getByRole('radiogroup', { name: '使用模式' }).boundingBox())!.height).toBe(24);
  expect(mode!.height).toBe(20);
  expect(mode!.width).toBe(24);
  await expect(header.getByRole('radio', { name: '团队', exact: true }).getByTitle('团队')).toBeVisible();
  const sidebar = page.getByRole('complementary', { name: '聊天记录' });
  const sidebarBounds = await sidebar.boundingBox();
  expect(toggle!.x).toBeCloseTo(sidebarBounds!.x + sidebarBounds!.width, 0);
  await header.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'chat-titlebar.png'), animations: 'disabled' });
  // New chat lives as a compact + on the list title's row, right of the label.
  const create = await sidebar.getByRole('button', { name: '新建聊天', exact: true }).boundingBox();
  const listLabel = await sidebar.getByText('聊天记录', { exact: true }).boundingBox();
  expect(Math.abs(listLabel!.y - create!.y)).toBeLessThan(8);
  expect(listLabel!.x).toBeLessThan(create!.x);
  expect(toggle!.height).toBe(36);
  expect(toggle!.width).toBe(28);
  expect((await bar.getByRole('button', { name: '收起聊天记录', exact: true }).locator('svg').boundingBox())!.height).toBe(20);
  expect((await bar.getByRole('button', { name: '收起聊天记录', exact: true }).locator('svg').boundingBox())!.width).toBe(16);
  const toggleX = toggle!.x;
  await bar.getByRole('button', { name: '收起聊天记录', exact: true }).click();
  expect((await bar.getByRole('button', { name: '展开聊天记录', exact: true }).boundingBox())!.x).toBeLessThan(toggleX);
  await expect(header.getByRole('radiogroup', { name: '使用模式' })).toBeHidden();
  expect((await bar.getByRole('button', { name: '展开聊天记录', exact: true }).boundingBox())!.height).toBe(36);
  await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, 'chat-collapsed.png'), animations: 'disabled' });
  await bar.getByRole('button', { name: '展开聊天记录', exact: true }).click();
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

test('desktop draws column heads and protects native controls from mode double clicks', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('cobot.mode', 'work');
    const target = window as unknown as { go: object; runtime: object; chromeMaximizeCalls: number };
    target.go = {};
    target.chromeMaximizeCalls = 0;
    target.runtime = {
      Environment: async () => ({ platform: 'windows' }),
      WindowIsMaximised: async () => false,
      WindowIsFullscreen: async () => false,
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
  expect((await page.getByRole('main').boundingBox())!.y).toBe(0);
  const sidebar = page.getByRole('complementary', { name: '团队列表' });
  const create = await sidebar.getByRole('button', { name: '新建团队', exact: true }).boundingBox();
  const listLabel = await sidebar.getByText('团队列表', { exact: true }).boundingBox();
  expect(Math.abs(listLabel!.y - create!.y)).toBeLessThan(8);
  expect(listLabel!.x).toBeLessThan(create!.x);
  await page.getByRole('radio', { name: '团队', exact: true }).dblclick();
  expect(await page.evaluate(() => (window as unknown as { chromeMaximizeCalls: number }).chromeMaximizeCalls)).toBe(0);
  await bar.dblclick({ position: { x: 350, y: 20 } });
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
      const button = document.querySelector<HTMLButtonElement>('[data-testid="app-title-bar"] button[aria-expanded]')!;
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

// The chat last opened is remembered by the page, and a window can come back
// to a data root that never had it (the desktop shell keeps that memory when
// the data root changes). It opens a new chat; it does not sit on "not found".
test('a remembered chat that is gone opens a new chat instead of an error', async ({ page, context }) => {
  await context.addInitScript(() => {
    localStorage.setItem('cobot.mode', 'chat');
    localStorage.setItem('cobot.chat.selected', '00000000-0000-4000-8000-000000000000');
  });
  await page.goto('/');
  await expect(page).toHaveURL(/\/chat\/new$/);
  await expect(page.getByTestId('chat-welcome')).toBeVisible();
  await expect(page.getByText('资源不存在')).toHaveCount(0);
});

for (const platform of ['web', 'windows', 'mac'] as const) {
  test(`${platform} keeps navigation controls on their column edges at every width`, async ({ page }) => {
    if (platform !== 'web') {
      await page.addInitScript((platform) => {
        const target = window as unknown as { go: object; runtime: object; chromeFullscreen: boolean };
        target.go = {};
        target.chromeFullscreen = false;
        target.runtime = {
          Environment: async () => ({ platform: platform === 'mac' ? 'darwin' : 'windows' }),
          WindowIsMaximised: async () => false,
          WindowIsFullscreen: async () => target.chromeFullscreen,
          WindowMinimise: () => undefined,
          WindowToggleMaximise: () => undefined,
          Quit: () => undefined,
        };
      }, platform);
    }
    await page.goto('/chat/new');
    const header = page.getByTestId('app-mode-header');
    const sidebar = page.getByRole('complementary', { name: '聊天记录', exact: true });
    const toggle = page.getByTestId('app-title-bar').getByRole('button', { name: /聊天记录/ });
    const modes = header.getByRole('radiogroup', { name: '使用模式' });
    let windowedToggleX = 0;
    for (const fullscreen of platform === 'mac' ? [false, true] : [false]) {
      await page.evaluate((fullscreen) => {
        (window as unknown as { chromeFullscreen: boolean }).chromeFullscreen = fullscreen;
        window.dispatchEvent(new Event('resize'));
      }, fullscreen);
      await expect(page.getByTestId('native-titlebar-inset')).toHaveCount(platform === 'mac' && !fullscreen ? 1 : 0);
      if (platform === 'mac' && !fullscreen) {
        // The reservation block covers the top of the rail and the sidebar
        // only; the conversation head starts at the window's top edge.
        const inset = (await page.getByTestId('native-titlebar-inset').boundingBox())!;
        const bar = (await page.getByTestId('app-title-bar').boundingBox())!;
        expect(inset.y).toBe(0);
        expect(inset.height).toBe(28);
        expect(inset.width).toBe(88);
        expect(inset.x).toBe(0);
        expect(bar.y).toBe(0);
        // The mac head rows are the titlebar's 28px metrics in both window
        // states, so nothing shifts across the fullscreen boundary.
        expect(bar.height).toBe(28);
      }
      if (platform === 'mac') {
        // While windowed the corner is reserved (logo below it); fullscreen
        // cancels the reservation and the top of the window stays in use.
        const logo = (await page.getByTestId('rail-logo').boundingBox())!;
        if (!fullscreen) expect(logo.y).toBeGreaterThanOrEqual(28);
        else expect(logo.y).toBeLessThan(28);
      }
      for (const width of [320, 390, 640, 1024, 1440]) {
        await page.setViewportSize({ width, height: 900 });
        for (const open of [true, false]) {
          await expect(toggle).toHaveAttribute('aria-expanded', String(open));
          // While macOS is windowed and the list is folded, the conversation
          // slides under the corner reservation and the toggle clears it by
          // the same 32px the sidebar head uses.
          const expectInset = platform === 'mac' && !fullscreen && !open ? 32 : 0;
          await expect.poll(async () => {
            const control = (await toggle.boundingBox())!;
            const canvas = (await page.getByRole('main').boundingBox())!;
            return Math.abs(control.x - canvas.x - expectInset);
          }).toBeLessThan(1);
          if (open) {
            const side = (await sidebar.boundingBox())!;
            const mode = (await modes.boundingBox())!;
            expect(mode.x + mode.width).toBeCloseTo(side.x + side.width - 8, 0);
            // The capsule lives inside the sidebar's head row (28px on the
            // mac desktop, 40px elsewhere).
            expect(mode.y + mode.height).toBeLessThanOrEqual(side.y + 40);
            // Toggling the reservation never shifts controls sideways.
            if (platform === 'mac' && width === 1024) {
              const x = (await toggle.boundingBox())!.x;
              if (!fullscreen) windowedToggleX = x;
              else expect(windowedToggleX).toBe(x);
            }
          } else {
            await expect(sidebar).toHaveCount(0);
            await expect(modes).toHaveCount(0);
          }
          expect(await page.locator('body').evaluate((element) => element.scrollWidth <= window.innerWidth)).toBe(true);
          if (open && [390, 1440].includes(width)) {
            await page.screenshot({ path: join(process.env.COBOT_E2E_ROOT!, `chrome-${platform}-${fullscreen ? 'full' : 'window'}-${width}.png`), animations: 'disabled' });
          }
          await toggle.click();
        }
      }
    }
    const logo = (await page.getByTestId('rail-logo').boundingBox())!;
    const home = (await page.getByRole('navigation', { name: 'CoBot', exact: true }).getByRole('button', { name: '新建聊天', exact: true }).boundingBox())!;
    expect(logo.y + logo.height).toBeLessThanOrEqual(home.y);
    await expect(header.locator('img')).toHaveCount(0);
    await expect(header.getByText('CoBot', { exact: true })).toHaveCount(0);
  });
}
