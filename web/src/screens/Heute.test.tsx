import { Route, Router } from "@solidjs/router";
import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { todayISO } from "../lib/dates";
import Heute from "./Heute";

afterEach(() => cleanup());

function renderHeute() {
  const today = todayISO();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      return new Response(
        JSON.stringify({
          belege: [
            {
              id: "beleg-1",
              datum: today,
              belegbetrag_cent: 380,
              berechnung: { erstattung_cent: 380 },
              bilder: [
                {
                  thumbnail_url: "/api/v1/belegbilder/beleg-1/thumbnail",
                  url: "/api/v1/belegbilder/beleg-1/datei",
                },
              ],
            },
          ],
          summen: { anzahl: 1, erstattung_cent: 380 },
          jahresregel: { monatslimit: 15 },
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }),
  );
  render(() => (
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <Router>
        <Route path="/" component={Heute} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("Heute", () => {
  it("shows the whole receipt in a portrait thumbnail", async () => {
    renderHeute();
    const img = (await screen.findByRole("img", {
      name: "Beleg",
    })) as HTMLImageElement;
    expect(img.className).toContain("object-contain");
    expect(img.className).not.toContain("object-cover");
    expect(img.className).toContain("h-40");
    expect(img.className).toContain("w-28");
    expect(img.getAttribute("src")).toBe(
      "/api/v1/belegbilder/beleg-1/thumbnail",
    );
    expect(screen.getByRole("link").getAttribute("href")).toBe(
      "/belege/beleg-1",
    );
  });

  it("opens the full receipt image from the thumbnail", async () => {
    renderHeute();
    fireEvent.click(
      await screen.findByRole("button", { name: "Belegbild öffnen" }),
    );
    const full = (await screen.findByRole("img", {
      name: "Belegbild",
    })) as HTMLImageElement;
    expect(full.className).toContain("object-contain");
    expect(full.getAttribute("src")).toBe("/api/v1/belegbilder/beleg-1/datei");
    expect(screen.getByRole("heading", { name: "Belegbild" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Schließen" })).toBeTruthy();
  });
});
