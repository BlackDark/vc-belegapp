import { render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { describe, expect, it } from "vitest";
import { CaptureInputs } from "./Heute";
import { PruefenForm } from "./Pruefen";

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
});

describe("Erfassen", () => {
  it("requests the rear camera", () => {
    render(() => <CaptureInputs onFile={() => undefined} />);
    expect(screen.getByLabelText("Kamera").getAttribute("capture")).toBe(
      "environment",
    );
  });
});
