import { defineConfig, devices } from '@playwright/test'
import { baseURL } from './e2e/env'

// The screenshots of the documentation landing page, taken on the server and
// the fixtures of the end-to-end tests (see e2e/global-setup.ts):
//
//   npm run screenshots
//
// They are written to docs/static/img/screenshots, one light and one dark
// WebP per view (ImageMagick `convert` is needed).
export default defineConfig({
  testDir: './e2e',
  testMatch: /screenshots\.ts$/,
  globalSetup: './e2e/global-setup.ts',
  outputDir: './e2e/.run/results',
  workers: 1,
  reporter: 'list',
  use: {
    ...devices['Desktop Chrome'],
    baseURL,
    locale: 'en-US',
    timezoneId: 'Europe/Paris',
    viewport: { width: 1280, height: 800 },
    deviceScaleFactor: 2,
  },
})
