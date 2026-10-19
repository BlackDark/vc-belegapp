import { expect, startStack, test } from "../fixtures";

// A browser behind a forward-auth proxy. The proxy authenticates it and writes
// who it is into a header; this spec plays both roles, because the stack lists
// the loopback address as the trusted network, so the headers this context
// sends are believed exactly as a proxy's would be.
const identitaet = "eduard@example.de";

test("a trusted header signs the browser in without a login", async ({
  browser,
}, info) => {
  const stack = await startStack({
    name: `header-${info.workerIndex}`,
    workerIndex: info.workerIndex,
    slot: 5,
    llm: false,
    header: { name: "Remote-Email", allowed: identitaet },
  });
  try {
    // The browser the proxy signed in: every request carries the header, and
    // the first one is enough — there is no password form in between.
    const proxied = await browser.newContext({
      extraHTTPHeaders: { "Remote-Email": identitaet },
    });
    try {
      const page = await proxied.newPage();
      await page.goto(stack.baseURL);
      await expect(page.getByRole("heading", { name: "Heute" })).toBeVisible();

      const me = await page.evaluate(async () => {
        const res = await fetch("/api/v1/auth/me", { credentials: "include" });
        return {
          status: res.status,
          body: (await res.json()) as { akteur?: string; methode?: string },
        };
      });
      expect(me.status).toBe(200);
      expect(me.body.akteur).toBe(`header:${identitaet}`);
      expect(me.body.methode).toBe("header");

      // The app cannot end a login that lives at the proxy, so the settings
      // do not pretend to: no logout button, and no URL was configured that
      // could stand in for one.
      await page.goto(`${stack.baseURL}/einstellungen`);
      await expect(
        page.getByRole("heading", { name: "Sitzungen" }),
      ).toBeVisible();
      await expect(page.getByRole("button", { name: "Abmelden" })).toHaveCount(
        0,
      );
      await expect(page.getByRole("link", { name: "Abmelden" })).toHaveCount(0);
    } finally {
      await proxied.close();
    }

    // The browser the proxy did not sign in. No header, no session, so the
    // login screen appears — and it says what is actually missing rather than
    // pretending a password would help.
    const unproxied = await browser.newContext();
    try {
      const page = await unproxied.newPage();
      await page.goto(`${stack.baseURL}/login`);
      await expect(page.getByText(/vorgeschalteten Proxy/)).toBeVisible();
    } finally {
      await unproxied.close();
    }
  } finally {
    await stack.stop();
  }
});

test("a header the allowlist does not cover is refused", async ({
  browser,
}, info) => {
  const stack = await startStack({
    name: `header-denied-${info.workerIndex}`,
    workerIndex: info.workerIndex,
    slot: 6,
    llm: false,
    header: { name: "Remote-Email", allowed: identitaet },
  });
  try {
    const fremd = await browser.newContext({
      extraHTTPHeaders: { "Remote-Email": "jemand@example.de" },
    });
    try {
      const page = await fremd.newPage();
      await page.goto(stack.baseURL);
      // 403 on /auth/me lands on the login screen with the reason the login
      // screen already words for the OIDC callback.
      await expect(
        page.getByText("Dieses Konto ist nicht berechtigt."),
      ).toBeVisible();
    } finally {
      await fremd.close();
    }
  } finally {
    await stack.stop();
  }
});
