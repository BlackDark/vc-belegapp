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
import { currentMonth } from "../lib/dates";
import Monat from "./Monat";

const monat = currentMonth();

function monatData(overrides: Record<string, unknown> = {}) {
  return {
    monat,
    status: "offen",
    letzte_exportversion: 0,
    jahresregel: null,
    tage: [],
    belege: [],
    summen: {
      anzahl: 1,
      belegbetrag_cent: 1490,
      anerkannt_cent: 1490,
      erstattung_cent: 745,
      eigenanteil_cent: 745,
      gv_cent: 0,
      steuerfrei_cent: 0,
      regulaer_cent: 745,
      pauschalsteuer_cent: 186,
      soli_cent: 9,
      kist_cent: 0,
      pauschal_gesamt_cent: 195,
      an_pflichtig_cent: 1490,
      ag_kosten_cent: 1685,
    },
    pruefpunkte: [{ code: "OK", ergebnis: "ok", text: "Alle Belege geprüft." }],
    warnungen: [],
    exporte: [],
    ...overrides,
  };
}

const einstellungen = {
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

type Calls = { url: string; body: unknown }[];

function stubApi(data: unknown, exportStatus = 200) {
  const calls: Calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const json = (value: unknown) =>
        new Response(JSON.stringify(value), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      if (url === `/api/v1/monate/${monat}`) {
        return json(data);
      }
      if (url === "/api/v1/einstellungen") {
        return json(einstellungen);
      }
      if (url === `/api/v1/monate/${monat}/exporte`) {
        calls.push({
          url,
          body: init?.body ? JSON.parse(String(init.body)) : null,
        });
        if (exportStatus === 200) {
          return json({ id: "export-1", version: 1, monat });
        }
        return new Response(
          JSON.stringify({
            code: exportStatus === 422 ? "E_PRUEFPUNKT_FEHLGESCHLAGEN" : "E_X",
            detail: "Prüfpunkte sind erneut fehlgeschlagen.",
          }),
          {
            status: exportStatus,
            headers: { "Content-Type": "application/json" },
          },
        );
      }
      if (url === `/api/v1/monate/${monat}/pruefpunkte`) {
        return json({
          pruefpunkte: [
            { code: "BILDER", ergebnis: "fehler", text: "Bild fehlt." },
          ],
        });
      }
      return new Response(JSON.stringify({ detail: "unexpected" }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  return calls;
}

function renderMonat() {
  const history = createMemoryHistory();
  history.set({ value: "/monat", replace: true });
  render(() => (
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter history={history}>
        <Route path="/monat" component={Monat} />
      </MemoryRouter>
    </QueryClientProvider>
  ));
}

async function openDialog() {
  fireEvent.click(await screen.findByRole("button", { name: "Exportieren" }));
  return screen.findByRole("dialog");
}

function finalButton(): HTMLButtonElement {
  return screen.getByRole("button", {
    name: "Final exportieren",
  }) as HTMLButtonElement;
}

function checkbox(name: string | RegExp): HTMLInputElement {
  return screen.getByRole("checkbox", { name }) as HTMLInputElement;
}

const erklaerung = /Ich versichere, dass jeder aufgeführte Beleg/;

// The dialog seeds CSV and ZIP from the settings query once it arrives.
async function waitForDefaults(): Promise<void> {
  await waitFor(() => {
    if (checkbox("CSV").checked !== true) {
      throw new Error("settings defaults not applied yet");
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

describe("Monat export dialog", () => {
  it("seeds CSV from the settings default and blocks until the declaration is ticked", async () => {
    stubApi(monatData());
    renderMonat();
    await openDialog();

    await waitForDefaults();
    expect(checkbox("ZIP mit Originalbildern").checked).toBe(false);
    expect(finalButton().disabled).toBe(true);

    fireEvent.click(checkbox(erklaerung));
    expect(finalButton().disabled).toBe(false);
  });

  it("sends the picked formats and closes on success", async () => {
    const calls = stubApi(monatData());
    renderMonat();
    await openDialog();

    await waitForDefaults();
    fireEvent.click(checkbox(erklaerung));
    fireEvent.click(checkbox("ZIP mit Originalbildern"));
    fireEvent.click(finalButton());

    await screen.findByRole("button", { name: "Exportieren" });
    expect(calls).toEqual([
      {
        url: `/api/v1/monate/${monat}/exporte`,
        body: {
          erklaerung_bestaetigt: true,
          warnungen_bestaetigt: false,
          csv: true,
          zip: true,
        },
      },
    ]);
  });

  it("requires the warning confirmation when warnings exist", async () => {
    const calls = stubApi(
      monatData({ warnungen: [{ code: "WOCHENENDE", text: "Samstag." }] }),
    );
    renderMonat();
    await openDialog();

    await waitForDefaults();
    fireEvent.click(checkbox(erklaerung));
    expect(finalButton().disabled).toBe(true);

    fireEvent.click(checkbox("Warnungen geprüft"));
    expect(finalButton().disabled).toBe(false);
    fireEvent.click(finalButton());
    await screen.findByRole("button", { name: "Exportieren" });
    expect(calls[0].body).toMatchObject({ warnungen_bestaetigt: true });
  });

  it("blocks the final export while a Pruefpunkt fails", async () => {
    stubApi(
      monatData({
        pruefpunkte: [
          { code: "BILDER", ergebnis: "fehler", text: "Bild fehlt." },
        ],
      }),
    );
    renderMonat();
    await openDialog();

    await waitForDefaults();
    fireEvent.click(checkbox(erklaerung));
    expect(finalButton().disabled).toBe(true);
    expect(
      screen.getByText(/Finaler Export ist blockiert, bis die mit ✗/),
    ).toBeTruthy();
  });

  it("reloads the Pruefpunkte when the export fails the strict check", async () => {
    stubApi(monatData(), 422);
    renderMonat();
    await openDialog();

    await waitForDefaults();
    fireEvent.click(checkbox(erklaerung));
    fireEvent.click(finalButton());

    expect(
      await screen.findByText("Prüfpunkte sind erneut fehlgeschlagen."),
    ).toBeTruthy();
    expect(screen.getByText("Bild fehlt.")).toBeTruthy();
    expect(finalButton().disabled).toBe(true);
  });
});
