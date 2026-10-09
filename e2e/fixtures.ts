import { type ChildProcess, spawn } from "node:child_process";
import { mkdir, readFile, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { expect, test as base, type Page } from "@playwright/test";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

// Low-cost PHC (m=8192, t=1, p=1) for the password "belegapp-e2e".
export const password = process.env.E2E_PASSWORD ?? "belegapp-e2e";
export const passwordHash =
  process.env.BELEGAPP_AUTH_PASSWORD_HASH ||
  "$argon2id$v=19$m=8192,t=1,p=1$ZTJlc2FsdGUyZXNhbHQ$fpwELKXJzuNANkbVeL78/95t50JZd5M7U094xSidoBM";

// 1×1 PNG. The server normalises it to JPEG and strips metadata.
export const png = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
  "base64",
);

export type Stack = {
  baseURL: string;
  stop: () => Promise<void>;
};

type StartOpts = {
  name: string;
  workerIndex: number;
  slot: number;
  llm?: boolean;
  oidc?: boolean;
};

export async function startStack(opts: StartOpts): Promise<Stack> {
  const llm = opts.llm !== false;
  const appPort = 22000 + opts.workerIndex * 20 + opts.slot;
  const llmPort = appPort + 1;
  const oidcPort = appPort + 2;
  const baseURL = `http://127.0.0.1:${appPort}`;
  const dataDir = path.join("/tmp", `belegapp-e2e-${opts.name}-${opts.slot}`);
  await rm(dataDir, { recursive: true, force: true });
  await mkdir(dataDir, { recursive: true });

  const children: ChildProcess[] = [];
  const logs: string[] = [];
  const track = (child: ChildProcess) => {
    const push = (chunk: Buffer) => {
      for (const line of chunk.toString().split("\n")) {
        if (!line) continue;
        logs.push(line);
        if (logs.length > 200) logs.shift();
      }
    };
    child.stdout?.on("data", push);
    child.stderr?.on("data", push);
    children.push(child);
    return child;
  };
  const tail = () => logs.join("\n");
  const stop = async () => {
    await Promise.all(children.map((child) => stopChild(child)));
  };

  try {
    if (llm) {
      const llmChild = track(spawnLogged(binOrGo(process.env.FAKE_LLM_BIN, ["./e2e/fakellm"]), {
        FAKE_LLM_ADDR: `127.0.0.1:${llmPort}`,
      }));
      await waitOk(`http://127.0.0.1:${llmPort}/healthz`, llmChild, tail);
    }

    const appEnv: NodeJS.ProcessEnv = {
      BELEGAPP_LISTEN_ADDR: `127.0.0.1:${appPort}`,
      BELEGAPP_DATA_DIR: dataDir,
      BELEGAPP_COOKIE_SECURE: "false",
      BELEGAPP_BASE_URL: baseURL,
      BELEGAPP_TZ: "Europe/Berlin",
      BELEGAPP_AUTH_PASSWORD_HASH: passwordHash,
      BELEGAPP_LOG_FORMAT: "text",
      BELEGAPP_LOG_LEVEL: "warn",
      BELEGAPP_JOB_WORKERS: "2",
    };
    if (llm) {
      appEnv.BELEGAPP_LLM_BASE_URL = `http://127.0.0.1:${llmPort}/v1`;
      appEnv.BELEGAPP_LLM_API_KEY = "e2e";
      appEnv.BELEGAPP_LLM_MODEL = "fake-vision";
      appEnv.BELEGAPP_LLM_RESPONSE_FORMAT = "json_schema";
      appEnv.BELEGAPP_LLM_TIMEOUT = "10s";
    } else {
      appEnv.BELEGAPP_LLM_ENABLED = "false";
    }
    if (opts.oidc) {
      const configPath = path.join("/tmp", `belegapp-oidc-${opts.name}-${opts.slot}.json`);
      await rm(configPath, { force: true });
      const oidcChild = track(
        spawnLogged(binOrGo(process.env.FAKE_OIDC_BIN, ["./e2e/mockoidc"]), {
          FAKE_OIDC_ADDR: `127.0.0.1:${oidcPort}`,
          FAKE_OIDC_SUBJECT: "e2e-user",
          FAKE_OIDC_CONFIG: configPath,
        }),
      );
      const cfg = await readOidcConfig(configPath, oidcChild, tail);
      appEnv.BELEGAPP_OIDC_ISSUER_URL = cfg.issuer;
      appEnv.BELEGAPP_OIDC_CLIENT_ID = cfg.client_id;
      appEnv.BELEGAPP_OIDC_CLIENT_SECRET = cfg.client_secret;
      appEnv.BELEGAPP_OIDC_ALLOWED_SUBJECTS = "e2e-user";
    }

    const appBin = process.env.BELEGAPP_BIN;
    const appCmd: Cmd = appBin
      ? { cmd: appBin, args: ["serve"] }
      : { cmd: "go", args: ["run", "./cmd/belegapp", "serve"] };
    const appChild = track(spawnLogged(appCmd, appEnv));
    await waitOk(`${baseURL}/healthz`, appChild, tail);
    if (opts.oidc) {
      await waitOidc(baseURL, appChild, tail);
    }
    return { baseURL, stop };
  } catch (err) {
    await stop();
    throw err;
  }
}

type Cmd = { cmd: string; args: string[] };

function binOrGo(bin: string | undefined, goRun: string[]): Cmd {
  if (bin) return { cmd: bin, args: [] };
  return { cmd: "go", args: ["run", ...goRun] };
}

function spawnLogged(command: Cmd, env: NodeJS.ProcessEnv): ChildProcess {
  const child = spawn(command.cmd, command.args, {
    cwd: repoRoot,
    env: { ...process.env, ...env },
    detached: true,
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.on("error", () => {
    // waitOk reports a dead process; ignore the duplicate event.
  });
  return child;
}

async function stopChild(child: ChildProcess) {
  if (!child.pid || child.exitCode !== null) return;
  const pid = child.pid;
  try {
    process.kill(-pid, "SIGTERM");
  } catch {
    try {
      child.kill("SIGTERM");
    } catch {
      return;
    }
  }
  await new Promise<void>((resolve) => {
    const timer = setTimeout(() => {
      try {
        process.kill(-pid, "SIGKILL");
      } catch {
        // already gone
      }
      resolve();
    }, 2_000);
    child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

async function waitOk(url: string, child: ChildProcess, tail: () => string) {
  const start = Date.now();
  let last = "no response";
  while (Date.now() - start < 90_000) {
    if (child.exitCode !== null) {
      throw new Error(`${url} process exited ${child.exitCode}\n${tail()}`);
    }
    try {
      const res = await fetch(url);
      if (res.ok) return;
      last = `HTTP ${res.status}`;
    } catch (err) {
      last = err instanceof Error ? err.message : String(err);
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`timed out waiting for ${url}: ${last}\n${tail()}`);
}

async function readOidcConfig(file: string, child: ChildProcess, tail: () => string) {
  const start = Date.now();
  while (Date.now() - start < 30_000) {
    if (child.exitCode !== null) {
      throw new Error(`mockoidc exited ${child.exitCode}\n${tail()}`);
    }
    try {
      const raw = await readFile(file, "utf8");
      return JSON.parse(raw) as { issuer: string; client_id: string; client_secret: string };
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
  }
  throw new Error(`mockoidc config missing\n${tail()}`);
}

async function waitOidc(baseURL: string, child: ChildProcess, tail: () => string) {
  const start = Date.now();
  let last = "";
  while (Date.now() - start < 30_000) {
    if (child.exitCode !== null) {
      throw new Error(`belegapp exited ${child.exitCode}\n${tail()}`);
    }
    try {
      const res = await fetch(`${baseURL}/api/v1/auth/config`);
      const body = (await res.json()) as { oidc?: boolean };
      if (body.oidc) return;
      last = JSON.stringify(body);
    } catch (err) {
      last = err instanceof Error ? err.message : String(err);
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`oidc discovery did not become ready: ${last}\n${tail()}`);
}

export const test = base.extend<{ app: Stack }>({
  app: async ({}, use, testInfo) => {
    const stack = await startStack({
      name: `w${testInfo.workerIndex}`,
      workerIndex: testInfo.workerIndex,
      slot: 0,
      llm: true,
    });
    await use(stack);
    await stack.stop();
  },
  baseURL: async ({ app }, use) => {
    await use(app.baseURL);
  },
});

export { expect };

export async function login(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Passwort").fill(password);
  await page.getByRole("button", { name: "Anmelden" }).click();
  await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();
}

export async function saveYear(page: Page, year: number) {
  await page.goto(`/einstellungen/jahre/${year}`);
  await expect(page.getByText("Zuschuss (Cent)")).toBeVisible();
  await page.getByRole("button", { name: "Speichern" }).click();
  await expect(page.getByText("Jahresregel gespeichert")).toBeVisible();
}
