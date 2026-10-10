import { createMemoryHistory, MemoryRouter, Route } from "@solidjs/router";
import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AuthConfig } from "../lib/api";
import Login from "./Login";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

type FetchLog = { url: string; body: unknown }[];

function stubFetch(config: AuthConfig, login: () => Response) {
  const log: FetchLog = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url === "/api/v1/auth/config") {
        return new Response(JSON.stringify(config), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (url === "/api/v1/auth/login") {
        log.push({
          url,
          body: init?.body ? JSON.parse(String(init.body)) : null,
        });
        return login();
      }
      return new Response(null, { status: 404 });
    }),
  );
  return log;
}

function renderLogin(url = "/login") {
  const history = createMemoryHistory();
  history.set({ value: url, replace: true });
  render(() => (
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter history={history}>
        <Route path="/login" component={Login} />
        <Route path="/" component={() => <p>Startseite</p>} />
      </MemoryRouter>
    </QueryClientProvider>
  ));
}

describe("Login", () => {
  it("shows the SSO button with the configured label", async () => {
    stubFetch(
      { passwort: true, oidc: true, oidc_label: "Mit Keycloak" },
      () => new Response(null, { status: 204 }),
    );
    renderLogin();
    expect(
      (await screen.findByRole("link", { name: "Mit Keycloak" })).getAttribute(
        "href",
      ),
    ).toBe("/api/v1/auth/oidc/start");
  });

  it("falls back to the default SSO label", async () => {
    stubFetch(
      { passwort: false, oidc: true, oidc_label: "" },
      () => new Response(null, { status: 204 }),
    );
    renderLogin();
    expect(
      await screen.findByRole("link", { name: "Mit SSO anmelden" }),
    ).toBeTruthy();
    expect(screen.queryByLabelText("Passwort")).toBeNull();
  });

  it("translates the fehler parameter from the OIDC callback", async () => {
    stubFetch(
      { passwort: true, oidc: true, oidc_label: "Mit SSO" },
      () => new Response(null, { status: 204 }),
    );
    renderLogin("/login?fehler=nicht_berechtigt");
    expect(
      await screen.findByText("Dieses Konto ist nicht berechtigt."),
    ).toBeTruthy();
  });

  it("ignores an unknown fehler parameter", async () => {
    stubFetch(
      { passwort: true, oidc: false, oidc_label: "" },
      () => new Response(null, { status: 204 }),
    );
    renderLogin("/login?fehler=irgendwas");
    await screen.findByLabelText("Passwort");
    expect(screen.queryByText(/nicht berechtigt/)).toBeNull();
  });

  it("logs in with the password and lands on the start page", async () => {
    const log = stubFetch(
      { passwort: true, oidc: false, oidc_label: "" },
      () => new Response(null, { status: 204 }),
    );
    renderLogin();
    fireEvent.input(await screen.findByLabelText("Passwort"), {
      target: { value: "geheim" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Anmelden" }));
    expect(await screen.findByText("Startseite")).toBeTruthy();
    expect(log).toEqual([
      { url: "/api/v1/auth/login", body: { passwort: "geheim" } },
    ]);
  });

  it("shows the problem detail when the login is rejected", async () => {
    stubFetch(
      { passwort: true, oidc: false, oidc_label: "" },
      () =>
        new Response(JSON.stringify({ detail: "Passwort falsch." }), {
          status: 401,
          headers: { "Content-Type": "application/json" },
        }),
    );
    renderLogin();
    fireEvent.input(await screen.findByLabelText("Passwort"), {
      target: { value: "falsch" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Anmelden" }));
    expect(await screen.findByText("Passwort falsch.")).toBeTruthy();
    expect(screen.queryByText("Startseite")).toBeNull();
  });

  it("reports an unreachable auth config", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(JSON.stringify({ detail: "offline" }), {
            status: 503,
            headers: { "Content-Type": "application/json" },
          }),
      ),
    );
    renderLogin();
    expect(
      await screen.findByText("Die Anmeldung ist gerade nicht erreichbar."),
    ).toBeTruthy();
  });
});
