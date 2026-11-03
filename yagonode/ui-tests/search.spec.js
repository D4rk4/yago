const { test, expect } = require('@playwright/test');

const query = process.env.YAGO_UI_QUERY || 'Canada parliament';
const searchPage = '/yacysearch.html?resource=local&contentdom=text';

async function observeSearchResultPage(page) {
  await expect(page.getByRole('heading', { name: `Search for ${query}` })).toBeVisible();
  const status = page.locator('.result-count');
  await expect(status).toContainText(/result|searching/i, { timeout: 15_000 });
  await expect(status).not.toContainText(/searching/i, { timeout: 20_000 });
  const results = page.locator('#search-results > li');
  const resultCount = await results.count();
  if (resultCount > 0) {
    await expect(results.first()).toBeVisible();
    await expect(results.first().getByRole('heading').getByRole('link')).toBeVisible();
  } else if (/partial|searching/i.test(await status.innerText())) {
    await expect(page.locator('#nothing-found')).toHaveCount(0);
  } else {
    await expect(page.locator('#nothing-found')).toBeVisible();
  }
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByText(/local search deadline exceeded|search incomplete/i)).toHaveCount(0);

  return { resultCount };
}

test('returns useful local results on progressive and repeated searches', async ({ page }, testInfo) => {
  test.setTimeout(90_000);
  await page.goto(searchPage);
  await page.locator('#query').fill(query);
  await page.getByRole('button', { name: 'search' }).click();
  await expect(page).toHaveURL(/progressive=1/);

  const first = await observeSearchResultPage(page);
  expect(
    first.resultCount,
    'the first progressive response should have a useful result',
  ).toBeGreaterThan(0);
  await page.screenshot({ path: testInfo.outputPath('first-search.png'), fullPage: true });

  // Exercise the repeat-search path without assuming a broad query's detached
  // full-shard completion finishes within its budget on a busy real corpus.
  // The Go tests cover complete-result cache admission and reuse.
  await page.waitForTimeout(5_000);
  await page.locator('#query').fill(query);
  await page.getByRole('button', { name: 'search' }).click();
  await expect(page).toHaveURL(/progressive=1/);
  const repeated = await observeSearchResultPage(page);
  expect(
    repeated.resultCount,
    'the repeated progressive response should keep useful results',
  ).toBeGreaterThan(0);
  await page.screenshot({ path: testInfo.outputPath('repeated-search.png'), fullPage: true });
});

test('does not claim an incomplete empty search is complete', async ({ page }, testInfo) => {
  const unmatched = `yagoui${Date.now()}qzxv`;
  await page.goto(searchPage);
  await page.locator('#query').fill(unmatched);
  await page.getByRole('button', { name: 'search' }).click();
  await expect(page).toHaveURL(/progressive=1/);

  await expect(page.getByRole('heading', { name: `Search for ${unmatched}` })).toBeVisible();
  const status = page.locator('.result-count');
  await expect(status).toContainText(/result|searching/i);
  await expect(status).not.toContainText(/searching/i, { timeout: 20_000 });
  await expect(page.getByRole('alert')).toHaveCount(0);
  await expect(page.getByText(/local search deadline exceeded|search incomplete/i)).toHaveCount(0);
  if (await status.innerText().then((text) => /partial/i.test(text))) {
    await expect(page.locator('#nothing-found')).toHaveCount(0);
  } else {
    await expect(page.locator('#nothing-found')).toBeVisible();
  }
  await page.screenshot({ path: testInfo.outputPath('empty-search.png'), fullPage: true });
});
