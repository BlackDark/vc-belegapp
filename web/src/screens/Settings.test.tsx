import { createMemoryHistory, MemoryRouter, Route } from "@solidjs/router";
import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import Settings from "./Settings";

const profile = {
  arbeitnehmer_name: "Eduard",
  personalnummer: "42",
  arbeitgeber_name: "Example GmbH",
  standard_bezugsort: "supermarkt",
  standard_arbeitsort: "betrieb",
  erkennung_aktiv: false,
  export_zip_standard: false,
  export_csv_standard: true,
  geaendert_am: "2026-10-08T10:00:00Z",
};

const info = {
  version: "1.1.1",
  commit: "abc",
  storage_backend: "fs",
  erkennung_konfiguriert: false,
  llm_model: "gpt-5-mini",
  llm_base_url: "https://api.openai.com/v1",
  llm_enabled: false,
  typst_version: "typst 0.15.1",
};

const werte = {
  sachbezugswerte: [],
  kirchensteuer: [{ land: "NW", satz_bp: 700 }],
  bundeslaender: ["NW"],
  mahlzeiten: [
    { code: "fruehstueck", label: "Frühstück" },
    { code: "mittag", label: "Mittag" },
    { code: "abend", label: "Abend" },
  ],
  bezugsorte: [{ code: "supermarkt", label: "Supermarkt" }],
};

const responses: Record<string, unknown> = {
  "/api/v1/einstellungen": profile,
  "/api/v1/jahresregeln": [{ jahr: 2026 }],
  "/api/v1/system/info": info,
  "/api/v1/auth/sitzungen": [],
  "/api/v1/auth/config": {
    passwort: true,
    oidc: false,
    oidc_label: "",
    header: false,
  },
  "/api/v1/auth/me": {
    akteur: "passwort",
    methode: "passwort",
    sitzung_seit: "2026-10-08T10:00:00Z",
  },
  "/api/v1/werte": werte,
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
  document.documentElement.classList.remove("dark");
  delete document.documentElement.dataset.theme;
});

function renderSettings(overrides: Record<string, unknown> = {}) {
  const table = { ...responses, ...overrides };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const body = table[url];
      if (body === undefined) {
        return new Response(JSON.stringify({ detail: "unexpected" }), {
          status: 404,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  const history = createMemoryHistory();
  history.set({ value: "/einstellungen", replace: true });
  render(() => (
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter history={history}>
        <Route path="/einstellungen" component={Settings} />
      </MemoryRouter>
    </QueryClientProvider>
  ));
}

describe("Settings theme", () => {
  it("marks the picked theme as the selected tab", async () => {
    renderSettings();
    fireEvent.click(await screen.findByRole("tab", { name: "Hell" }));
    expect(
      screen.getByRole("tab", { name: "Hell" }).getAttribute("aria-selected"),
    ).toBe("true");
    expect(
      screen.getByRole("tab", { name: "Dunkel" }).getAttribute("aria-selected"),
    ).toBe("false");
  });

  it("stores the picked theme and toggles the document class", async () => {
    localStorage.setItem("belegapp-theme", "dark");
    renderSettings();
    fireEvent.click(await screen.findByRole("tab", { name: "Hell" }));
    expect(localStorage.getItem("belegapp-theme")).toBe("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(document.documentElement.dataset.theme).toBe("light");

    fireEvent.click(screen.getByRole("tab", { name: "Dunkel" }));
    expect(localStorage.getItem("belegapp-theme")).toBe("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("keeps System on the system preference", async () => {
    vi.stubGlobal("matchMedia", () => ({
      matches: true,
      media: "(prefers-color-scheme: dark)",
      addEventListener: () => {},
      removeEventListener: () => {},
    }));
    localStorage.setItem("belegapp-theme", "light");
    renderSettings();
    fireEvent.click(await screen.findByRole("tab", { name: "System" }));
    expect(localStorage.getItem("belegapp-theme")).toBe("system");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("keeps a legacy German theme value working", async () => {
    vi.stubGlobal("matchMedia", () => ({
      matches: false,
      media: "(prefers-color-scheme: dark)",
      addEventListener: () => {},
      removeEventListener: () => {},
    }));
    localStorage.setItem("belegapp-theme", "dunkel");
    renderSettings();
    fireEvent.click(await screen.findByRole("tab", { name: "Hell" }));
    expect(localStorage.getItem("belegapp-theme")).toBe("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });
});

describe("Settings password", () => {
  it("sends the current password with the new one and clears the form", async () => {
    const sent: { method: string; body: unknown }[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url === "/api/v1/auth/passwort") {
          sent.push({
            method: init?.method ?? "GET",
            body: JSON.parse(String(init?.body)),
          });
          return new Response(null, { status: 204 });
        }
        const body = responses[url];
        if (body === undefined) {
          return new Response(null, { status: 404 });
        }
        return new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    const history = createMemoryHistory();
    history.set({ value: "/einstellungen", replace: true });
    render(() => (
      <QueryClientProvider
        client={
          new QueryClient({ defaultOptions: { queries: { retry: false } } })
        }
      >
        <MemoryRouter history={history}>
          <Route path="/einstellungen" component={Settings} />
        </MemoryRouter>
      </QueryClientProvider>
    ));

    fireEvent.input(await screen.findByLabelText("Aktuelles Passwort"), {
      target: { value: "altes-langes-passwort" },
    });
    fireEvent.input(screen.getByLabelText("Neues Passwort"), {
      target: { value: "neues-langes-passwort" },
    });
    fireEvent.input(screen.getByLabelText("Neues Passwort wiederholen"), {
      target: { value: "neues-langes-passwort" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Passwort ändern" }));

    await vi.waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({
      method: "PUT",
      body: {
        aktuell: "altes-langes-passwort",
        neu: "neues-langes-passwort",
      },
    });
    await vi.waitFor(() =>
      expect(
        (screen.getByLabelText("Aktuelles Passwort") as HTMLInputElement).value,
      ).toBe(""),
    );
  });

  it("refuses a new password that was typed twice differently", async () => {
    renderSettings();
    fireEvent.input(await screen.findByLabelText("Neues Passwort"), {
      target: { value: "neues-langes-passwort" },
    });
    fireEvent.input(screen.getByLabelText("Neues Passwort wiederholen"), {
      target: { value: "etwas-anderes-langes" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Passwort ändern" }));
    // The two fields already disagree, so the card asks the server for nothing.
    const fetchMock = globalThis.fetch as unknown as {
      mock: { calls: unknown[][] };
    };
    expect(
      fetchMock.mock.calls.some((call) => call[0] === "/api/v1/auth/passwort"),
    ).toBe(false);
  });

  // An instance that has no password — OIDC only, or a proxy that asserts the
  // identity — has nothing to change here, and a card that asks for a current
  // password nobody ever set can only end in a wrong error message.
  it("is not offered when the instance has no password", async () => {
    renderSettings({
      "/api/v1/auth/config": {
        passwort: false,
        oidc: true,
        oidc_label: "Mit SSO",
        header: false,
      },
    });
    expect(await screen.findByText("Sitzungen")).toBeTruthy();
    expect(screen.queryByLabelText("Aktuelles Passwort")).toBeNull();
  });
});

describe("Settings logout", () => {
  // A browser the proxy signed in cannot end that login here, so the app does
  // not pretend to: it offers the proxy's own logout instead.
  it("links to the proxy logout instead of ending the session", async () => {
    renderSettings({
      "/api/v1/auth/config": {
        passwort: false,
        oidc: false,
        oidc_label: "",
        header: true,
        header_logout_url: "https://auth.example.de/logout",
      },
      "/api/v1/auth/me": {
        akteur: "header:eduard@example.de",
        methode: "header",
        sitzung_seit: "2026-10-08T10:00:00Z",
      },
    });
    const link = await screen.findByRole("link", { name: "Abmelden" });
    expect(link.getAttribute("href")).toBe("https://auth.example.de/logout");
    expect(screen.queryByRole("button", { name: "Abmelden" })).toBeNull();
  });

  // Without a URL the button simply goes away: a logout that the proxy undoes
  // on the next request is worse than no button.
  it("offers no logout at all when the proxy has no logout URL", async () => {
    renderSettings({
      "/api/v1/auth/config": {
        passwort: false,
        oidc: false,
        oidc_label: "",
        header: true,
      },
      "/api/v1/auth/me": {
        akteur: "header:eduard@example.de",
        methode: "header",
        sitzung_seit: "2026-10-08T10:00:00Z",
      },
    });
    expect(await screen.findByText("Sitzungen")).toBeTruthy();
    // Nothing on this screen shows that the /auth/me query has landed, so the
    // absence below is only meaningful once it has: wait for the request, then
    // for the answer to reach the component.
    const fetchMock = globalThis.fetch as unknown as {
      mock: { calls: unknown[][] };
    };
    await vi.waitFor(() =>
      expect(
        fetchMock.mock.calls.some((call) => call[0] === "/api/v1/auth/me"),
      ).toBe(true),
    );
    await vi.waitFor(() =>
      expect(screen.queryByRole("button", { name: "Abmelden" })).toBeNull(),
    );
    expect(screen.queryByRole("link", { name: "Abmelden" })).toBeNull();
  });
});
