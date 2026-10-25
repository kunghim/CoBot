import { randomUUID } from 'node:crypto';
import { test, expect, type Page } from '@playwright/test';
import { authorize } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

async function saved(page: Page, action: () => Promise<unknown>, patch: Record<string, string>) {
  const [response] = await Promise.all([
    page.waitForResponse((response) => response.request().method() === 'PATCH' && response.url().endsWith('/api/v1/settings')),
    action(),
  ]);
  expect(response.ok(), await response.text()).toBeTruthy();
  expect((await response.json()).data).toMatchObject(patch);
}

// Complements language and avatar specs. Every change checks the stored
// response as well as the page, so a transient local preview cannot pass.
test('browser appearance saves all palettes and follows system mode after reload', async ({ page, context }) => {
  const original = (await (await context.request.get('/api/v1/settings')).json()).data;
  const status = (await (await context.request.get('/api/v1/status')).json()).data;
  const csrf = (await context.cookies()).find((cookie) => cookie.name === status.csrfCookieName)!.value;
  const patchSettings = async (patch: Record<string, string>) => {
    const current = (await (await context.request.get('/api/v1/settings')).json()).data;
    const response = await context.request.patch('/api/v1/settings', {
      headers: { 'X-CSRF-Token': csrf },
      data: { requestId: randomUUID(), expectedRevision: current.revision, patch },
    });
    expect(response.ok(), await response.text()).toBeTruthy();
  };
  try {
    await patchSettings({ colorMode: 'system', theme: 'classic' });
    await page.emulateMedia({ colorScheme: 'light' });
    await page.goto('/settings/appearance');
    const html = page.locator('html');
    await saved(page, () => page.getByRole('radio', { name: /^(深色|Dark)$/ }).click(), { colorMode: 'dark' });
    await expect(html).toHaveAttribute('data-mode', 'dark');
    await saved(page, () => page.getByRole('radio', { name: /^(浅色|Light)$/ }).click(), { colorMode: 'light' });
    await expect(html).toHaveAttribute('data-mode', 'light');
    for (const [theme, name] of [
      ['graphite', /石墨|Graphite/], ['harbor', /港湾|Harbor/],
      ['dusk', /暮梅|Dusk/], ['classic', /经典|Classic/],
    ] as const) {
      await saved(page, () => page.getByRole('radio', { name }).click(), { theme });
      await page.reload();
      await expect(html).toHaveAttribute('data-theme', theme);
      await expect(page.getByRole('radio', { name })).toHaveAttribute('aria-checked', 'true');
    }
    await saved(page, () => page.getByRole('radio', { name: /^(跟随系统|System)$/ }).click(), { colorMode: 'system' });
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect(html).toHaveAttribute('data-mode', 'dark');
    await page.reload();
    await expect(page.getByRole('radio', { name: /^(跟随系统|System)$/ })).toHaveAttribute('aria-checked', 'true');
    await expect(html).toHaveAttribute('data-mode', 'dark');
    await page.emulateMedia({ colorScheme: 'light' });
    await expect(html).toHaveAttribute('data-mode', 'light');
  } finally {
    await patchSettings({ colorMode: original.colorMode, theme: original.theme });
  }
});
