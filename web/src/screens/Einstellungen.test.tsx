import { createMemoryHistory, MemoryRouter, Route } from "@solidjs/router";
import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import Einstellungen from "./Einstellungen";

const profil = {
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

const responses: Record<string, unknown> = {
  "/api/v1/einstellungen": profil,
  "/api/v1/jahresregeln": [{ jahr: 2026 }],
  "/api/v1/system/info": info,
  "/api/v1/auth/sitzungen": [],
};

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  localStorage.clear();
  document.documentElement.classList.remove("dark");
  delete document.documentElement.dataset.theme;
});

function renderEinstellungen() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      const body = responses[url];
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
        <Route path="/einstellungen" component={Einstellungen} />
      </MemoryRouter>
    </QueryClientProvider>
  ));
}

describe("Einstellungen theme", () => {
  it("marks the picked theme as the selected tab", async () => {
    renderEinstellungen();
    fireEvent.click(await screen.findByRole("tab", { name: "Hell" }));
    expect(
      screen.getByRole("tab", { name: "Hell" }).getAttribute("aria-selected"),
    ).toBe("true");
    expect(
      screen.getByRole("tab", { name: "Dunkel" }).getAttribute("aria-selected"),
    ).toBe("false");
  });

  it("stores the picked theme and toggles the document class", async () => {
    localStorage.setItem("belegapp-theme", "dunkel");
    renderEinstellungen();
    fireEvent.click(await screen.findByRole("tab", { name: "Hell" }));
    expect(localStorage.getItem("belegapp-theme")).toBe("hell");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(document.documentElement.dataset.theme).toBe("hell");

    fireEvent.click(screen.getByRole("tab", { name: "Dunkel" }));
    expect(localStorage.getItem("belegapp-theme")).toBe("dunkel");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("keeps System on the system preference", async () => {
    vi.stubGlobal("matchMedia", () => ({
      matches: true,
      media: "(prefers-color-scheme: dark)",
      addEventListener: () => {},
      removeEventListener: () => {},
    }));
    localStorage.setItem("belegapp-theme", "hell");
    renderEinstellungen();
    fireEvent.click(await screen.findByRole("tab", { name: "System" }));
    expect(localStorage.getItem("belegapp-theme")).toBe("system");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });
});
