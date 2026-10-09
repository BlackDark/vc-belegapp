import { defineConfig, devices } from "@playwright/test";

const chromium = { ...devices["Desktop Chrome"], browserName: "chromium" as const };

// Each test starts its own belegapp (see fixtures.ts), so workers do not share a database.
export default defineConfig({
  testDir: "./tests",
  fullyParallel: true,
  workers: process.env.CI ? 4 : 2,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  timeout: 120_000,
  expect: { timeout: 15_000 },
  use: {
    serviceWorkers: "block",
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "chromium",
      testIgnore: /screenshots\.spec\.ts/,
      use: { ...chromium, viewport: { width: 1280, height: 800 } },
    },
    {
      name: "desktop",
      testMatch: /screenshots\.spec\.ts/,
      use: { ...chromium, viewport: { width: 1280, height: 800 } },
    },
    {
      name: "mobile",
      testMatch: /screenshots\.spec\.ts/,
      use: {
        ...devices["iPhone 15"],
        browserName: "chromium",
        viewport: { width: 393, height: 852 },
      },
    },
  ],
});
