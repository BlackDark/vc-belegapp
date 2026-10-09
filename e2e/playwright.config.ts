import { defineConfig, devices } from "@playwright/test";

const port = process.env.E2E_PORT ?? "8080";
const importPort = process.env.E2E_PORT_IMPORT ?? "8081";
const llmPort = process.env.E2E_LLM_PORT ?? "18081";
const baseURL = `http://127.0.0.1:${port}`;
const importURL = `http://127.0.0.1:${importPort}`;
const llmURL = `http://127.0.0.1:${llmPort}`;
// Low-cost PHC (m=8192, t=1, p=1) for the password "belegapp-e2e".
// hash-password still emits the production parameters.
const passwordHash =
  process.env.BELEGAPP_AUTH_PASSWORD_HASH ||
  "$argon2id$v=19$m=8192,t=1,p=1$ZTJlc2FsdGUyZXNhbHQ$fpwELKXJzuNANkbVeL78/95t50JZd5M7U094xSidoBM";

export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  use: {
    baseURL,
    serviceWorkers: "block",
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "iphone-15",
      use: {
        ...devices["iPhone 15"],
        browserName: "chromium",
        viewport: { width: 393, height: 852 },
      },
    },
    {
      name: "pixel-8",
      use: {
        ...devices["Pixel 8"],
        browserName: "chromium",
        viewport: { width: 412, height: 915 },
      },
    },
  ],
  webServer: [
    {
      command: "go run ./e2e/fakellm",
      cwd: "..",
      url: `${llmURL}/healthz`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      env: {
        FAKE_LLM_ADDR: `127.0.0.1:${llmPort}`,
      },
    },
    {
      command: process.env.BELEGAPP_BIN
        ? `${process.env.BELEGAPP_BIN} serve`
        : "go run ./cmd/belegapp serve",
      cwd: "..",
      url: `${baseURL}/healthz`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      env: {
        BELEGAPP_LISTEN_ADDR: `127.0.0.1:${port}`,
        BELEGAPP_DATA_DIR: process.env.BELEGAPP_DATA_DIR ?? "/tmp/belegapp-e2e",
        BELEGAPP_COOKIE_SECURE: "false",
        BELEGAPP_BASE_URL: baseURL,
        BELEGAPP_TZ: "Europe/Berlin",
        BELEGAPP_AUTH_PASSWORD_HASH: passwordHash,
        BELEGAPP_LLM_BASE_URL: `${llmURL}/v1`,
        BELEGAPP_LLM_API_KEY: "e2e",
        BELEGAPP_LLM_MODEL: "fake-vision",
        BELEGAPP_LLM_RESPONSE_FORMAT: "json_schema",
        BELEGAPP_LLM_TIMEOUT: "10s",
        BELEGAPP_JOB_WORKERS: "2",
      },
    },
    {
      command: process.env.BELEGAPP_BIN
        ? `${process.env.BELEGAPP_BIN} serve`
        : "go run ./cmd/belegapp serve",
      cwd: "..",
      url: `${importURL}/healthz`,
      reuseExistingServer: !process.env.CI,
      timeout: 120_000,
      env: {
        BELEGAPP_LISTEN_ADDR: `127.0.0.1:${importPort}`,
        BELEGAPP_DATA_DIR:
          process.env.BELEGAPP_DATA_DIR_IMPORT ?? "/tmp/belegapp-e2e-import",
        BELEGAPP_COOKIE_SECURE: "false",
        BELEGAPP_BASE_URL: importURL,
        BELEGAPP_TZ: "Europe/Berlin",
        BELEGAPP_AUTH_PASSWORD_HASH: passwordHash,
        BELEGAPP_LLM_ENABLED: "false",
        BELEGAPP_JOB_WORKERS: "1",
      },
    },
  ],
});
