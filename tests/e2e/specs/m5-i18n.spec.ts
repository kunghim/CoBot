import { test, expect } from '@playwright/test';
import { authorize } from './helpers';

test.beforeEach(async ({ context }) => {
  await authorize(context);
});

// I04: the language setting really switches the whole UI between zh-CN and
// en, and the saved choice persists. Model output is never translated by the
// product, so only chrome labels change.
test('language setting switches the UI and persists', async ({ page }) => {
  await page.goto('/settings');
  await expect(page.getByRole('navigation', { name: /设置|Settings/ })).toBeVisible();

  // Settings save on change; there is no separate Save button.
  await page.getByRole('combobox', { name: /语言|Language/i }).click();
  await page.getByRole('option', { name: 'English' }).click();

  // The page re-renders in English: the same labels change language.
  await expect(page.getByRole('navigation', { name: 'Settings' })).toBeVisible({ timeout: 15000 });
  await expect(page.getByRole('link', { name: 'Security' })).toBeVisible();

  // The choice persists across a reload.
  await page.reload();
  await expect(page.getByRole('navigation', { name: 'Settings' })).toBeVisible({ timeout: 15000 });
  await expect(page.getByRole('combobox', { name: 'Language' })).toHaveText(/English/);

  // Switch back so the rest of the suite keeps its Chinese defaults.
  await page.getByRole('combobox', { name: 'Language' }).click();
  await page.getByRole('option', { name: '简体中文' }).click();
  await expect(page.getByRole('navigation', { name: '设置' })).toBeVisible({ timeout: 15000 });
});
