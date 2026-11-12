const { test, expect } = require('@playwright/test');

test('public portal exposes the search form', async ({ page }) => {
  const response = await page.goto('/yacysearch.html?resource=local&contentdom=text');
  expect(response.status()).toBeLessThan(400);
  await expect(page.locator('#query')).toBeVisible();
  await expect(page.getByRole('button', { name: /search/i })).toBeVisible();
});

test('public portal links to the agent API guide', async ({ page }) => {
  const response = await page.goto('/');
  expect(response.status()).toBeLessThan(400);
  const guideLink = page.getByRole('link', { name: 'Use the agent API' });
  await expect(guideLink).toHaveAttribute('href', '/api');
  await guideLink.click();
  await expect(page.getByRole('heading', { name: 'Use the agent API' })).toBeVisible();
  await expect(page.getByText('search:read', { exact: false })).toBeVisible();
});

test('public local search accepts a synthetic query without reporting an error', async ({ page }) => {
  const query = `dev-smoke-${Date.now().toString(36)}`;
  const response = await page.goto('/yacysearch.html?resource=local&contentdom=text');
  expect(response.status()).toBeLessThan(400);
  await page.locator('#query').fill(query);
  await page.getByRole('button', { name: /search/i }).click();
  await expect(page).toHaveURL(/progressive=1/);
  await expect(page.locator('h1')).toContainText(query);
  const status = page.locator('.result-count');
  await expect(status).toContainText(/result|searching/i);
  await expect(status).not.toContainText(/searching/i);
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByText(/local search deadline exceeded|search incomplete/i)).toHaveCount(0);
});
