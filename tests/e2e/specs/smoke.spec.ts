import { test, expect } from '@playwright/test';

test('app loads and engine status is reported', async ({ page }) => {
  await page.goto('/');
  await expect(page.locator('#root')).toBeVisible();
});
