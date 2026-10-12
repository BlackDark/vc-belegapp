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
import { afterEach, describe, expect, it, vi } from "vitest";
import Unassigned from "./Unassigned";

const bild = (id: string, overrides: Record<string, unknown> = {}) => ({
  id,
  erstellt_am: "2026-10-08T13:00:00Z",
  erkennung_status: "keine",
  erkennung_fehler: null,
  thumbnail_url: `/api/v1/belegbilder/${id}/thumbnail`,
  faellig_am: null,
  ...overrides,
});

const werte = {
  sachbezugswerte: [],
  kirchensteuer: [{ land: "NW", satz_bp: 700 }],
  bundeslaender: ["NW"],
  mahlzeiten: [{ code: "mittag", label: "Mittag" }],
  bezugsorte: [{ code: "supermarkt", label: "Supermarkt" }],
};

// The screen is rendered inside a MemoryRouter so the deep link can be
// followed without a server. Calls are recorded to assert the request path.
type Calls = string[];

function stubApi(status = 200, body?: unknown, deletes: string[] = []): Calls {
  const calls: Calls = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      calls.push(`${init?.method ?? "GET"} ${String(url)}`);
      if (url === "/api/v1/werte") {
        return new Response(JSON.stringify(werte), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      if (String(url).startsWith("/api/v1/belegbilder/offen")) {
        return new Response(
          JSON.stringify(
            body ?? { bilder: [], gesamt: 0, limit: 25, offset: 0 },
          ),
          { status, headers: { "Content-Type": "application/json" } },
        );
      }
      if (init?.method === "DELETE") {
        deletes.push(String(url));
        return new Response(null, { status: 204 });
      }
      return new Response(JSON.stringify({ detail: "unexpected" }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  return calls;
}

function renderUnassigned() {
  const history = createMemoryHistory();
  history.set({ value: "/offene-bilder", replace: true });
  render(() => (
    <QueryClientProvider
      client={
        new QueryClient({ defaultOptions: { queries: { retry: false } } })
      }
    >
      <MemoryRouter history={history}>
        <Route path="/offene-bilder" component={Unassigned} />
        <Route path="/belege/neu" component={() => <p>Beleg erfassen</p>} />
      </MemoryRouter>
      <Toaster />
    </QueryClientProvider>
  ));
  return history;
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

// A response that never arrives keeps the query pending, so the loading
// branch is observable without racing the happy path.
function stubPending(): void {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => new Promise<Response>(() => undefined)),
  );
}

describe("Unassigned", () => {
  it("shows the skeleton while the list loads", async () => {
    stubPending();
    renderUnassigned();
    await waitFor(() => {
      expect(
        document.querySelectorAll("[aria-hidden='true']").length,
      ).toBeGreaterThan(0);
    });
    // Neither the empty state nor a row appears before the data does.
    expect(screen.queryByText("Keine offenen Bilder")).toBeNull();
    expect(screen.queryByRole("link", { name: "Prüfen" })).toBeNull();
  });

  it("says so when nothing is open", async () => {
    stubApi();
    renderUnassigned();
    expect(await screen.findByText("Keine offenen Bilder")).toBeTruthy();
    expect(screen.queryByRole("link", { name: "Prüfen" })).toBeNull();
  });

  it("reports a failed load instead of an empty list", async () => {
    stubApi(500, { detail: "Datenbank nicht erreichbar" });
    renderUnassigned();
    expect(
      await screen.findByText(
        "Die offenen Bilder konnten nicht geladen werden.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("Keine offenen Bilder")).toBeNull();
  });

  it("lists thumbnail, upload time, and status badge", async () => {
    stubApi(200, {
      bilder: [
        bild("bild-1", {
          erkennung_status: "fertig",
          erstellt_am: "2026-10-08T13:00:00Z",
        }),
        bild("bild-2", {
          erkennung_status: "fehler",
          erkennung_fehler: "Modell nicht erreichbar",
        }),
      ],
      gesamt: 2,
      limit: 25,
      offset: 0,
      ttl_stunden: 24,
    });
    renderUnassigned();

    const thumbnails = await screen.findAllByRole("img", { name: "Beleg" });
    expect(thumbnails).toHaveLength(2);
    expect((thumbnails[0] as HTMLImageElement).getAttribute("src")).toBe(
      "/api/v1/belegbilder/bild-1/thumbnail",
    );
    expect(thumbnails[0].className).toContain("object-contain");
    expect(await screen.findByText("Erkannt")).toBeTruthy();
    expect(await screen.findByText("Erkennung fehlgeschlagen")).toBeTruthy();
    expect(await screen.findByText("Modell nicht erreichbar")).toBeTruthy();
    // Every entry states when it arrived, in German date order.
    const times = screen.getAllByText(/Hochgeladen am \d{2}\.\d{2}\.\d{4}, /);
    expect(times).toHaveLength(2);
  });

  it("asks for the first page of the list endpoint", async () => {
    const calls = stubApi(200, {
      bilder: [bild("bild-1")],
      gesamt: 1,
      limit: 25,
      offset: 0,
    });
    renderUnassigned();
    await screen.findByRole("link", { name: "Prüfen" });
    expect(calls).toContain("GET /api/v1/belegbilder/offen?limit=25&offset=0");
  });

  it("links each entry into the review form with the bild deep link", async () => {
    stubApi(200, {
      bilder: [bild("bild-1")],
      gesamt: 1,
      limit: 25,
      offset: 0,
    });
    const history = renderUnassigned();
    const link = await screen.findByRole("link", { name: "Prüfen" });
    expect(link.getAttribute("href")).toBe("/belege/neu?bild=bild-1");
    fireEvent.click(link);
    await screen.findByText("Beleg erfassen");
    expect(history.get()).toContain("/belege/neu?bild=bild-1");
  });

  // The deadline copy must follow BELEGAPP_UNASSIGNED_IMAGE_TTL, not a number
  // baked into the build.
  it("states the TTL the server reports", async () => {
    stubApi(200, {
      bilder: [bild("bild-1")],
      gesamt: 1,
      limit: 25,
      offset: 0,
      ttl_stunden: 72,
    });
    renderUnassigned();
    await screen.findByRole("link", { name: "Prüfen" });
    expect(document.body.textContent).toContain(
      "Ohne Zuordnung werden sie nach 72 Stunden entfernt.",
    );
  });

  it("uses the singular for a one-hour TTL", async () => {
    stubApi(200, {
      bilder: [bild("bild-1")],
      gesamt: 1,
      limit: 25,
      offset: 0,
      ttl_stunden: 1,
    });
    renderUnassigned();
    await screen.findByRole("link", { name: "Prüfen" });
    expect(document.body.textContent).toContain(
      "Ohne Zuordnung werden sie nach einer Stunde entfernt.",
    );
  });

  it("says nothing about removal when no sweeper runs", async () => {
    stubApi(200, {
      bilder: [bild("bild-1")],
      gesamt: 1,
      limit: 25,
      offset: 0,
      ttl_stunden: 0,
    });
    renderUnassigned();
    await screen.findByRole("link", { name: "Prüfen" });
    expect(document.body.textContent).not.toContain("Ohne Zuordnung");
  });

  // A row released from a deleted Beleg carries its original upload time and a
  // deadline that already passed; the row has to say so rather than look
  // freshly uploaded.
  it("marks an overdue row as overdue", async () => {
    stubApi(200, {
      bilder: [
        bild("bild-1", {
          erstellt_am: "2026-09-01T08:00:00Z",
          faellig_am: "2026-09-01T09:00:00Z",
        }),
      ],
      gesamt: 1,
      limit: 25,
      offset: 0,
      ttl_stunden: 1,
    });
    renderUnassigned();
    await screen.findByRole("link", { name: "Prüfen" });
    expect(document.body.textContent).toContain(
      "Entfernung überfällig, der nächste Durchlauf löscht das Bild",
    );
  });

  it("shows the deadline the server computed", async () => {
    const faellig = new Date(Date.now() + 5 * 3_600_000).toISOString();
    stubApi(200, {
      bilder: [bild("bild-1", { faellig_am: faellig })],
      gesamt: 1,
      limit: 25,
      offset: 0,
      ttl_stunden: 5,
    });
    renderUnassigned();
    await screen.findByRole("link", { name: "Prüfen" });
    expect(document.body.textContent).toMatch(
      /Entfernung am \d{2}\.\d{2}\.\d{4}, \d{2}:\d{2}/,
    );
  });

  // Without paging, everything past the first page is invisible and the sweeper
  // then deletes it for good.
  it("pages to the images beyond the first page", async () => {
    const calls = stubApi(200, {
      bilder: [bild("bild-1"), bild("bild-2")],
      gesamt: 60,
      limit: 25,
      offset: 0,
      ttl_stunden: 24,
    });
    renderUnassigned();
    await screen.findAllByRole("link", { name: "Prüfen" });
    expect(await screen.findByText("2 von 60 Bildern")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Weiter" }));
    await waitFor(() => {
      expect(calls).toContain(
        "GET /api/v1/belegbilder/offen?limit=25&offset=25",
      );
    });
  });

  it("hides the page controls when everything fits on one page", async () => {
    stubApi(200, {
      bilder: [bild("bild-1")],
      gesamt: 1,
      limit: 25,
      offset: 0,
    });
    renderUnassigned();
    await screen.findByRole("link", { name: "Prüfen" });
    expect(screen.queryByRole("button", { name: "Weiter" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Zurück" })).toBeNull();
  });

  // The copy promises "öffnen oder löschen", so the delete has to exist. It
  // uses the existing DELETE /belegbilder/{id}, no new endpoint.
  it("deletes an image through the existing endpoint", async () => {
    const deletes: string[] = [];
    const calls = stubApi(
      200,
      {
        bilder: [bild("bild-1"), bild("bild-2")],
        gesamt: 2,
        limit: 25,
        offset: 0,
      },
      deletes,
    );
    renderUnassigned();
    const buttons = await screen.findAllByRole("button", { name: "Löschen" });
    expect(buttons).toHaveLength(2);

    fireEvent.click(buttons[0]);
    await waitFor(() => {
      expect(deletes).toEqual(["/api/v1/belegbilder/bild-1"]);
    });
    // The list is invalidated so the count and the rows stay honest.
    await waitFor(() => {
      expect(
        calls.filter(
          (c) => c === "GET /api/v1/belegbilder/offen?limit=25&offset=0",
        ),
      ).toHaveLength(2);
    });
  });

  // Deleting the last row of the last page drops gesamt below the current
  // offset. Without a clamp the screen claims "keine offenen Bilder" while 25
  // rows sit on page one.
  it("falls back to an earlier page when the last page empties", async () => {
    const ids = Array.from({ length: 26 }, (_, i) => `bild-${i}`);
    const calls: Calls = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"} ${String(url)}`);
        const parsed = new URL(String(url), "http://test");
        if (parsed.pathname === "/api/v1/werte") {
          return new Response(JSON.stringify(werte), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (parsed.pathname === "/api/v1/belegbilder/offen") {
          const offset = Number(parsed.searchParams.get("offset") ?? 0);
          return new Response(
            JSON.stringify({
              bilder: ids.slice(offset, offset + 25).map((id) => bild(id)),
              gesamt: ids.length,
              limit: 25,
              offset,
              ttl_stunden: 24,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (init?.method === "DELETE") {
          ids.splice(ids.indexOf(String(url).split("/").pop() ?? ""), 1);
          return new Response(null, { status: 204 });
        }
        return new Response(JSON.stringify({ detail: "unexpected" }), {
          status: 404,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    renderUnassigned();
    await screen.findAllByRole("link", { name: "Prüfen" });

    fireEvent.click(screen.getByRole("button", { name: "Weiter" }));
    await waitFor(() => {
      expect(
        calls.filter((c) => c.includes("offset=25") && c.startsWith("GET")),
      ).not.toHaveLength(0);
    });
    expect(await screen.findByText("1 von 26 Bildern")).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: "Löschen" }));
    // The last row is gone, so the screen has to walk back to the page that
    // still holds rows instead of reporting an empty list.
    await waitFor(() => {
      expect(screen.queryByText("Keine offenen Bilder")).toBeNull();
    });
    await waitFor(() => {
      expect(
        calls.filter(
          (c) => c === "GET /api/v1/belegbilder/offen?limit=25&offset=0",
        ),
      ).toHaveLength(2);
    });
    await waitFor(() => {
      expect(screen.getAllByRole("link", { name: "Prüfen" })).toHaveLength(25);
    });
  });

  it("reports a failed delete instead of dropping the row silently", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (String(url).startsWith("/api/v1/belegbilder/offen")) {
          return new Response(
            JSON.stringify({
              bilder: [bild("bild-1")],
              gesamt: 1,
              limit: 25,
              offset: 0,
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (init?.method === "DELETE") {
          return new Response(
            JSON.stringify({ code: "E_BILD_UNBEKANNT", detail: "Zugeordnet." }),
            { status: 422, headers: { "Content-Type": "application/json" } },
          );
        }
        return new Response(JSON.stringify({ detail: "unexpected" }), {
          status: 404,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    renderUnassigned();
    fireEvent.click(await screen.findByRole("button", { name: "Löschen" }));
    await waitFor(() => {
      expect(screen.getByText("Zugeordnet.")).toBeTruthy();
    });
  });
});
