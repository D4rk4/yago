const { defineConfig, devices } = require('@playwright/test');

module.exports = defineConfig({
  testDir: '.',
  testMatch: 'live-smoke.spec.js',
  fullyParallel: false,
  workers: 1,
  timeout: 30_000,
  expect: { timeout: 20_000 },
  reporter: 'list',
  outputDir: 'test-results/live-smoke',
  use: {
    baseURL: process.env.YAGO_LIVE_BASE_URL || 'https://yago.zvorygin.ca',
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
  projects: [
    { name: 'live-chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'live-firefox', use: { ...devices['Desktop Firefox'] } },
  ],
});
