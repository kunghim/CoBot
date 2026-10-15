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
  await expect(page.getByRole('heading', { name: /设置|Settings/ })).toBeVisible();

  const languageSelect = page.getByRole('combobox', { name: /语言|Language/i });
  await languageSelect.selectOption('en');
  await page.getByRole('button', { name: /保存|Save/i }).click();

  // The page re-renders in English: the same labels change language.
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 15000 });
  await expect(page.getByRole('heading', { name: 'Live authorizations' })).toBeVisible();

  // The choice persists across a reload.
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible({ timeout: 15000 });
  await expect(page.getByRole('combobox', { name: 'Language' })).toHaveValue('en');

  // Switch back so the rest of the suite keeps its Chinese defaults.
  await page.getByRole('combobox', { name: 'Language' }).selectOption('zh-CN');
  await page.getByRole('button', { name: 'Save' }).click();
  await expect(page.getByRole('heading', { name: /设置/ })).toBeVisible({ timeout: 15000 });
});
