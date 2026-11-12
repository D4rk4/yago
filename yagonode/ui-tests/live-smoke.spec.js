const { test, expect } = require('@playwright/test');

test('public portal exposes the search form', async ({ page }) => {
  const response = await page.goto('/yacysearch.html?resource=local&contentdom=text');
  expect(response.status()).toBeLessThan(400);
  await expect(page.locator('#query')).toBeVisible();
  await expect(page.getByRole('button', { name: /search/i })).toBeVisible();
});

test('public root search returns a useful result window', async ({ page }) => {
  const query = process.env.YAGO_PORTAL_UI_QUERY || 'yago';
  await page.goto('/');
  await page.locator('form[role="search"] input[name="q"]').fill(query);
  await page.getByRole('button', { name: 'Search' }).click();

  const results = page.locator('.result');
  await expect(results.first()).toBeVisible({ timeout: 20_000 });
  await expect(results.first().locator('a.title')).toBeVisible();
  await expect(page.getByText('No results are currently available')).toHaveCount(0);
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

test('agent API accepts keyless basic search and keeps raw content protected', async ({ request }) => {
  const basic = await request.post('/search', {
    data: { query: 'agent-api-free-tier-smoke', max_results: 1 },
  });
  expect([200, 409]).toContain(basic.status());

  const raw = await request.post('/search', {
    data: { query: 'agent-api-raw-scope-smoke', include_raw_content: true },
  });
  expect(raw.status()).toBe(401);
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
