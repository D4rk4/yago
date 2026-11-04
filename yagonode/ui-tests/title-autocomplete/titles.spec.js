const { test, expect } = require('@playwright/test');

const query = process.env.YAGO_TITLE_ARRAY_QUERY || 'Compassionate Love';
const title = 'Compassionate Love | Encyclopedia.com';
const autocompleteQuery = process.env.YAGO_AUTOCOMPLETE_QUERY || 'mo';

test('renders the imported title and matching description on the YaCy results page', async ({ page }, testInfo) => {
  test.setTimeout(60_000);
  const searchURL = new URL('/yacysearch.html', process.env.YAGO_UI_BASE_URL || 'http://127.0.0.1:8080');
  searchURL.searchParams.set('query', query);
  searchURL.searchParams.set('resource', 'local');
  searchURL.searchParams.set('maximumRecords', '50');
  await page.goto(searchURL.toString());

  const row = page.locator('#search-results > li').filter({
    has: page.getByRole('link', { name: title, exact: true }),
  }).first();
  await expect(row).toBeVisible({ timeout: 15_000 });

  const titleText = (await row.getByRole('link', { name: title, exact: true }).innerText()).trim();
  const description = (await row.locator('p').first().innerText()).trim();
  expect(titleText).toBe(title);
  expect(description).toBe(title);
  expect(titleText).not.toMatch(/^\[\s*["']/);
  expect(description).not.toMatch(/^\[\s*["']/);
  await page.screenshot({ path: testInfo.outputPath('normalized-result.png'), fullPage: true });
});

test('keeps the main portal result window readable', async ({ page }, testInfo) => {
  test.setTimeout(60_000);
  await page.goto(`/?q=${encodeURIComponent(query)}`);
  const rows = page.locator('li.result');
  await expect(rows.first()).toBeVisible({ timeout: 15_000 });
  const titles = await rows.locator('a.title').allInnerTexts();
  expect(titles.length).toBeGreaterThan(0);
  for (const resultTitle of titles) {
    expect(resultTitle.trim()).not.toMatch(/^\[\s*["']/);
  }
  await page.screenshot({ path: testInfo.outputPath('portal-result-window.png'), fullPage: true });
});

test('autocomplete returns clean titles and supports keyboard and pointer selection', async ({ page }, testInfo) => {
  test.setTimeout(60_000);
  await page.goto('/');
  const input = page.getByRole('combobox', { name: 'Search query' });
  const responsePromise = page.waitForResponse((candidate) => {
    const url = new URL(candidate.url());
    return url.pathname === '/suggest.json' && url.searchParams.get('timeout') === '1000';
  });
  await input.fill(autocompleteQuery);
  const response = await responsePromise;
  expect(response.status()).toBe(200);
  const payload = await response.json();
  expect(Array.isArray(payload)).toBe(true);
  expect(Array.isArray(payload[1])).toBe(true);
  expect(payload[1].length).toBeGreaterThan(0);
  for (const suggestion of payload[1]) {
    expect(typeof suggestion).toBe('string');
    expect(suggestion.trim()).not.toMatch(/^\[\s*["']/);
  }

  const options = page.getByRole('listbox', { name: 'Search suggestions' }).getByRole('option');
  await expect(options.first()).toBeVisible();
  const keyboardSelection = (await options.first().innerText()).trim();
  await input.press('ArrowDown');
  await expect(input).toHaveAttribute('aria-activedescendant', 'ac-opt-0');
  await input.press('Enter');
  await expect(page).toHaveURL(/\?q=/);
  expect(new URL(page.url()).searchParams.get('q')).toBe(keyboardSelection);

  await page.goto('/');
  await input.fill(autocompleteQuery);
  await expect(options.first()).toBeVisible();
  const pointerSelection = (await options.first().innerText()).trim();
  await options.first().click();
  await expect(page).toHaveURL(/\?q=/);
  expect(new URL(page.url()).searchParams.get('q')).toBe(pointerSelection);
  await page.screenshot({ path: testInfo.outputPath('autocomplete-selected.png'), fullPage: true });
});
