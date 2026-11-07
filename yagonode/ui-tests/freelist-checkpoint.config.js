const { defineConfig, devices } = require('@playwright/test');

module.exports = defineConfig({
  testDir: '.',
  testMatch: 'freelist-checkpoint.spec.js',
  fullyParallel: false,
  workers: 1,
  timeout: 120_000,
  expect: { timeout: 20_000 },
  reporter: 'list',
  outputDir: process.env.YAGO_CHECKPOINT_OUTPUT_DIR || '/tmp/yago-freelist-checkpoint-ui',
  use: {
    baseURL: process.env.YAGO_CHECKPOINT_BASE_URL || 'http://127.0.0.1:18090',
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
  projects: [
    { name: 'checkpoint-chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'checkpoint-firefox', use: { ...devices['Desktop Firefox'] } },
  ],
});
