import { createMemoryHistory, MemoryRouter, Route } from "@solidjs/router";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Toaster } from "solid-sonner";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import AnnualRule from "./AnnualRule";

function regel(jahr: number, overrides: Record<string, unknown> = {}) {
  return {
    jahr,
    zuschuss_cent: 5000,
    mahlzeiten: ["fruehstueck", "mittag", "abend"],
    standard_mahlzeit: "mittag",
    sbw_fruehstueck_cent: 100,
    sbw_mittag_cent: 200,
    sbw_abend_cent: 300,
    hoechstzuschuss_aufschlag_cent: 0,
    pauschalierung: true,
    pauschsteuersatz_bp: 1200,
    soli_satz_bp: 550,
    gehaltsumwandlung: false,
    bundesland: "NW",
    kist_satz_bp: 0,
    eigenanteil_variante: "standard",
    monatslimit: 60,
    limit_modus: "warnen",
    eigene_feiertage: [],
    notiz: "",
    ...overrides,
  };
}

type Calls = { url: string; method: string; body: unknown }[];

// A missing entry makes that URL answer with a 500 so the query goes to error;
// an explicit undefined entry does the same for a URL that has a default.
type Responses = Record<string, unknown>;

const werte = {
  sachbezugswerte: [
    {
      jahr: 2025,
      fruehstueck_cent: 230,
      mittag_cent: 440,
      abend_cent: 440,
      status: "amtlich",
    },
    {
      jahr: 2026,
      fruehstueck_cent: 237,
      mittag_cent: 457,
      abend_cent: 457,
      status: "amtlich",
    },
    {
      jahr: 2027,
      fruehstueck_cent: 243,
      mittag_cent: 470,
      abend_cent: 470,
      status: "entwurf",
    },
  ],
  kirchensteuer: [
    { land: "BW", satz_bp: 450 },
    { land: "BE", satz_bp: 500 },
    { land: "NW", satz_bp: 700 },
  ],
  bundeslaender: ["BW", "BE", "NW"],
  mahlzeiten: [
    { code: "fruehstueck", label: "Frühstück" },
    { code: "mittag", label: "Mittag" },
    { code: "abend", label: "Abend" },
  ],
  bezugsorte: [{ code: "supermarkt", label: "Supermarkt" }],
};

function stubApi(responses: Responses) {
  const calls: Calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : null;
      calls.push({ url, method, body });
      // An explicit undefined entry answers with a 500, so a test can fail a
      // single URL, /werte included.
      const payload =
        url in responses
          ? responses[url]
          : url === "/api/v1/werte"
            ? werte
            : undefined;
      if (payload === undefined) {
        return new Response(JSON.stringify({ detail: "nope" }), {
          status: 500,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify(payload), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  return calls;
}

function renderAnnualRule(jahr: number) {
  const history = createMemoryHistory();
  history.set({ value: `/jahresregeln/${jahr}`, replace: true });
  render(() => (
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter history={history}>
        <Route path="/jahresregeln/:jahr" component={AnnualRule} />
      </MemoryRouter>
      <Toaster />
    </QueryClientProvider>
  ));
}

function number(label: string): HTMLInputElement {
  return screen.getByLabelText(label) as HTMLInputElement;
}

function trigger(label: string): HTMLElement {
  return screen.getByLabelText(label, { selector: "button" });
}

function checkbox(name: string | RegExp): HTMLInputElement {
  return screen.getByRole("checkbox", { name }) as HTMLInputElement;
}

// The Kobalte select opens on a pointer sequence and picks an option on pointer up.
async function choose(label: string, option: string) {
  fireEvent.pointerDown(trigger(label), { button: 0, ctrlKey: false });
  fireEvent.click(trigger(label));
  const item = await screen.findByRole("option", { name: option });
  fireEvent.pointerDown(item, { button: 0, ctrlKey: false });
  fireEvent.pointerUp(item, { button: 0, ctrlKey: false });
  fireEvent.click(item);
}

async function waitForEditor(): Promise<void> {
  await waitFor(() => {
    if (number("Zuschuss (Cent)").value !== "5000") {
      throw new Error("editor not seeded yet");
    }
  });
}

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe("Annual rule editor", () => {
  it("seeds the editor from the stored rule", async () => {
    const calls = stubApi({ "/api/v1/jahresregeln/2026": regel(2026) });
    renderAnnualRule(2026);

    expect(
      await screen.findByRole("heading", { name: "Jahresregel 2026" }),
    ).toBeTruthy();
    await waitForEditor();
    expect(number("SBW Frühstück").value).toBe("100");
    expect(number("SBW Mittag").value).toBe("200");
    expect(number("SBW Abend").value).toBe("300");
    expect(number("Monatslimit").value).toBe("60");
    expect(checkbox("Frühstück").checked).toBe(true);
    expect(checkbox("Mittag").checked).toBe(true);
    expect(checkbox("Abend").checked).toBe(true);
    expect(checkbox("Pauschalierung").checked).toBe(true);
    expect(checkbox("Gehaltsumwandlung").checked).toBe(false);
    expect(trigger("Bundesland").textContent).toContain("NW");
    expect(
      calls.filter((call) => call.url.endsWith("/vorschlag")),
    ).toHaveLength(0);
  });

  it("falls back to the suggested rule when the stored rule fails", async () => {
    const calls = stubApi({
      "/api/v1/jahresregeln/2026/vorschlag": regel(2026, {
        zuschuss_cent: 111,
      }),
    });
    renderAnnualRule(2026);

    await waitFor(() => {
      expect(number("Zuschuss (Cent)").value).toBe("111");
    });
    expect(calls.map((call) => call.url)).toContain(
      "/api/v1/jahresregeln/2026/vorschlag",
    );
  });

  it("offers a retry when neither the rule nor the suggestion load", async () => {
    stubApi({});
    renderAnnualRule(2026);

    expect(
      await screen.findByText("Die Jahresregel konnte nicht geladen werden."),
    ).toBeTruthy();
    expect(
      screen.getByRole("button", { name: "Erneut versuchen" }),
    ).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "Werte" })).toBeNull();
  });

  it("writes the official Sachbezugswerte into the SBW fields", async () => {
    stubApi({ "/api/v1/jahresregeln/2027": regel(2027) });
    renderAnnualRule(2027);
    await waitForEditor();

    fireEvent.click(
      screen.getByRole("button", { name: "Amtliche Werte übernehmen" }),
    );

    await waitFor(() => {
      expect(number("SBW Frühstück").value).toBe("243");
    });
    expect(number("SBW Mittag").value).toBe("470");
    expect(number("SBW Abend").value).toBe("470");
  });

  it("says the year has no official values only when the table loaded", async () => {
    stubApi({ "/api/v1/jahresregeln/2029": regel(2029) });
    renderAnnualRule(2029);
    await waitForEditor();

    const uebernehmen = screen.getByRole("button", {
      name: "Amtliche Werte übernehmen",
    }) as HTMLButtonElement;
    expect(uebernehmen.disabled).toBe(false);
    fireEvent.click(uebernehmen);
    expect(
      await screen.findByText(
        "Für dieses Jahr liegen keine amtlichen Werte vor.",
      ),
    ).toBeTruthy();
  });

  // A failed /werte is not a year without values: the button must stay disabled
  // so the screen never claims 2026 has no official values.
  it("disables the official-values button when the values table failed", async () => {
    stubApi({
      "/api/v1/jahresregeln/2026": regel(2026),
      "/api/v1/werte": undefined,
    });
    renderAnnualRule(2026);
    await waitForEditor();

    await waitFor(() => {
      const button = screen.getByRole("button", {
        name: "Amtliche Werte übernehmen",
      }) as HTMLButtonElement;
      expect(button.disabled).toBe(true);
    });
    expect(
      await screen.findByText(
        "Die Referenzwerte konnten nicht geladen werden. Codes werden stattdessen angezeigt.",
      ),
    ).toBeTruthy();
  });

  it("labels the meals with their raw codes when the values table failed", async () => {
    stubApi({
      "/api/v1/jahresregeln/2026": regel(2026),
      "/api/v1/werte": undefined,
    });
    renderAnnualRule(2026);
    await waitForEditor();

    // Every checkbox still carries a label, so the fieldset is never blank.
    expect(checkbox("fruehstueck").checked).toBe(true);
    expect(checkbox("mittag").checked).toBe(true);
    expect(checkbox("abend").checked).toBe(true);
  });

  it("moves the standard meal to the first remaining meal when it is unchecked", async () => {
    const calls = stubApi({ "/api/v1/jahresregeln/2026": regel(2026) });
    renderAnnualRule(2026);
    await waitForEditor();

    fireEvent.click(checkbox("Mittag"));
    await waitFor(() => {
      expect(checkbox("Mittag").checked).toBe(false);
    });
    fireEvent.click(screen.getByRole("button", { name: "Speichern" }));

    await waitFor(() => {
      expect(calls.some((call) => call.method === "PUT")).toBe(true);
    });
    expect(calls.find((call) => call.method === "PUT")?.body).toMatchObject({
      mahlzeiten: ["fruehstueck", "abend"],
      standard_mahlzeit: "fruehstueck",
    });
  });

  it("proposes the Kirchensteuer rate of the picked Bundesland", async () => {
    stubApi({ "/api/v1/jahresregeln/2026": regel(2026) });
    renderAnnualRule(2026);
    await waitForEditor();
    expect(number("Kirchensteuer (Basispunkte)").value).toBe("0");

    await choose("Bundesland", "BE");

    await waitFor(() => {
      expect(number("Kirchensteuer (Basispunkte)").value).toBe("500");
    });
    expect(trigger("Bundesland").textContent).toContain("BE");
  });

  it("PUTs the edited draft to the year from the route", async () => {
    const calls = stubApi({ "/api/v1/jahresregeln/2026": regel(2026) });
    renderAnnualRule(2026);
    await waitForEditor();

    fireEvent.input(number("Zuschuss (Cent)"), {
      target: { value: "6000" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Speichern" }));

    await waitFor(() => {
      expect(calls.some((call) => call.method === "PUT")).toBe(true);
    });
    const put = calls.find((call) => call.method === "PUT");
    expect(put?.url).toBe("/api/v1/jahresregeln/2026");
    expect(put?.body).toMatchObject({ jahr: 2026, zuschuss_cent: 6000 });
  });
});
