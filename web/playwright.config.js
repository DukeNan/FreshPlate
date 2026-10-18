import { defineConfig } from '@playwright/test'
export default defineConfig({
  testDir: './e2e', timeout: 45000, expect: { timeout: 10000 }, fullyParallel: false, workers: 1,
  reporter: [['list'], ['json', { outputFile: 'playwright-report/results.json' }]],
  use: { baseURL: 'http://127.0.0.1:18080', browserName: 'chromium', headless: true, launchOptions: process.env.FRESHPLATE_E2E_CHROMIUM ? { executablePath: process.env.FRESHPLATE_E2E_CHROMIUM } : {}, trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  webServer: { command: 'node e2e/server.mjs', url: 'http://127.0.0.1:18080/login', reuseExistingServer: false, timeout: 180000, gracefulShutdown: { signal: 'SIGTERM', timeout: 15000 } },
})
