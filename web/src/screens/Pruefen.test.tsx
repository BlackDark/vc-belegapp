import { cleanup, render, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { CaptureInputs } from "../components/CaptureInputs";
import type { Erkennung } from "../lib/api";
import { PruefenForm } from "./PruefenForm";

function field(root: ParentNode, name: string): HTMLInputElement {
  const label = [...root.querySelectorAll("label")].find(
    (node) => node.textContent?.trim() === name,
  );
  const id = label?.getAttribute("for");
  const input = id
    ? root.querySelector<HTMLInputElement>(`#${CSS.escape(id)}`)
    : null;
  if (!input) {
    throw new Error(name);
  }
  return input;
}

const initial = {
  datum: "2026-10-05",
  mahlzeit: "mittag",
  bezugsort: "supermarkt",
  arbeitsort: "betrieb",
  haendler_name: "Edeka",
  haendler_ort: "Köln",
  betrag: "14,90",
  korrigiert: "",
  korrektur_grund: "",
  notiz: "",
};

afterEach(() => cleanup());

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ detail: "offline" }), {
          status: 404,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
});

describe("Prüfen", () => {
  it("shows the manual receipt fields", () => {
    render(() => (
      <QueryClientProvider client={new QueryClient()}>
        <PruefenForm
          initial={initial}
          bildIds={["abc"]}
          onDone={() => undefined}
        />
      </QueryClientProvider>
    ));
    expect(screen.getByLabelText("Belegbetrag")).toBeTruthy();
    expect(screen.getByLabelText("Händler")).toBeTruthy();
    expect(screen.getByText("Speichern")).toBeTruthy();
  });

  it("keeps the correction suggestion until the user accepts it", async () => {
    const erkennung: Erkennung = {
      status: "fertig",
      korrekturvorschlag_cent: 1275,
      ergebnis: {
        ist_kassenbeleg: true,
        datum: "2026-10-07",
        uhrzeit: "12:30",
        haendler_name: "Edeka",
        haendler_ort: "Köln",
        gesamtbetrag_cent: 1490,
        waehrung: "EUR",
        positionen: [
          { bezeichnung: "Brot", betrag_cent: 1000, kategorie: "lebensmittel" },
          { bezeichnung: "Shampoo", betrag_cent: 215, kategorie: "nonfood" },
        ],
        bezugsort_vorschlag: "supermarkt",
        konfidenz: 0.92,
        hinweise: null,
      },
    };
    render(() => (
      <QueryClientProvider client={new QueryClient()}>
        <PruefenForm
          initial={initial}
          bildIds={["abc"]}
          erkennung={erkennung}
          onDone={() => undefined}
        />
      </QueryClientProvider>
    ));
    expect(screen.getByTestId("korrekturvorschlag")).toBeTruthy();
    const betrag = field(document.body, "Anerkannter Betrag");
    const grund = field(document.body, "Grund");
    expect(betrag.value).toBe("");
    expect(grund.value).toBe("");
    screen.getByRole("button", { name: "Übernehmen" }).click();
    await waitFor(() => {
      expect(field(document.body, "Anerkannter Betrag").value).toBe("12,75");
      expect(field(document.body, "Grund").value).toBe(
        "Automatisch: ohne Pfand/Alkohol/Tabak/Non-Food",
      );
    });
  });
});

describe("Erfassen", () => {
  it("requests the rear camera", () => {
    render(() => <CaptureInputs onFile={() => undefined} />);
    expect(screen.getByLabelText("Kamera").getAttribute("capture")).toBe(
      "environment",
    );
  });
});
