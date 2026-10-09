import { expect, startStack, test } from "../fixtures";

test("oidc login against the mock provider", async ({ page }, info) => {
  const stack = await startStack({
    name: `oidc-${info.workerIndex}`,
    workerIndex: info.workerIndex,
    slot: 4,
    llm: false,
    oidc: true,
  });
  try {
    await page.goto(`${stack.baseURL}/login`);
    await expect(page.getByRole("link", { name: "Mit SSO anmelden" })).toBeVisible();
    await page.getByRole("link", { name: "Mit SSO anmelden" }).click();
    await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();
    const me = await page.evaluate(async () => {
      const res = await fetch("/api/v1/auth/me", { credentials: "include" });
      return { status: res.status, body: (await res.json()) as { akteur?: string } };
    });
    expect(me.status).toBe(200);
    expect(me.body.akteur).toBe("oidc:e2e-user");
  } finally {
    await stack.stop();
  }
});
