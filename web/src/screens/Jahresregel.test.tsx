import { createMemoryHistory, MemoryRouter, Route } from "@solidjs/router";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Jahresregel from "./Jahresregel";

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

// A missing entry makes that URL answer with a 500 so the query goes to error.
type Responses = Record<string, unknown>;

function stubApi(responses: Responses) {
  const calls: Calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const method = init?.method ?? "GET";
      const body = init?.body ? JSON.parse(String(init.body)) : null;
      calls.push({ url, method, body });
      const payload = responses[url];
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

function renderJahresregel(jahr: number) {
  const history = createMemoryHistory();
  history.set({ value: `/jahresregeln/${jahr}`, replace: true });
  render(() => (
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter history={history}>
        <Route path="/jahresregeln/:jahr" component={Jahresregel} />
      </MemoryRouter>
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

describe("Jahresregel editor", () => {
  it("seeds the editor from the stored rule", async () => {
    const calls = stubApi({ "/api/v1/jahresregeln/2026": regel(2026) });
    renderJahresregel(2026);

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
    renderJahresregel(2026);

    await waitFor(() => {
      expect(number("Zuschuss (Cent)").value).toBe("111");
    });
    expect(calls.map((call) => call.url)).toContain(
      "/api/v1/jahresregeln/2026/vorschlag",
    );
  });

  it("offers a retry when neither the rule nor the suggestion load", async () => {
    stubApi({});
    renderJahresregel(2026);

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
    renderJahresregel(2027);
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

  it("moves the standard meal to the first remaining meal when it is unchecked", async () => {
    const calls = stubApi({ "/api/v1/jahresregeln/2026": regel(2026) });
    renderJahresregel(2026);
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
    renderJahresregel(2026);
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
    renderJahresregel(2026);
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
