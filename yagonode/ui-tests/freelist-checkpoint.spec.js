const { test, expect } = require('@playwright/test');
const path = require('node:path');

test('checkpoints vault freelists while the node remains ready', async ({ page, browserName }, testInfo) => {
  const username = process.env.YAGO_ADMIN_USER;
  const password = process.env.YAGO_ADMIN_PASSWORD;
  if (!username || !password) {
    throw new Error('YAGO_ADMIN_USER and YAGO_ADMIN_PASSWORD are required for the local admin scenario');
  }

  await page.goto('/admin/configuration');
  const usernameField = page.locator('input[name="username"]');
  if (await usernameField.isVisible()) {
    await usernameField.fill(username);
    await page.locator('input[name="password"]').fill(password);
    await page.getByRole('button', { name: 'Sign in' }).click();
    await page.waitForLoadState('domcontentloaded');
    await page.goto('/admin/configuration');
  }

  await page.getByRole('tab', { name: 'Storage' }).click();
  const saveButton = page.getByRole('button', { name: 'Save freelists now' });
  await expect(saveButton).toBeVisible();
  const before = path.join(testInfo.outputDir, `storage-before-${browserName}.png`);
  await page.screenshot({ path: before, fullPage: true });

  await saveButton.click();
  await expect(page.getByText(/Saved freelists for \d+ shard\(s\)\. Vault operations have resumed\./))
    .toBeVisible({ timeout: 120_000 });
  await expect(page.getByRole('tab', { name: 'Storage' })).toHaveAttribute('aria-selected', 'true');
  await expect(saveButton).toBeVisible();
  const after = path.join(testInfo.outputDir, `storage-after-${browserName}.png`);
  await page.screenshot({ path: after, fullPage: true });

  const readinessURL = process.env.YAGO_CHECKPOINT_READINESS_URL || '/ready';
  const readiness = await page.request.get(readinessURL);
  expect(readiness.ok()).toBeTruthy();
  expect((await readiness.json()).ready).toBeTruthy();
});
