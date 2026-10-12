import { A } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createMemo, createSignal, For, Show } from "solid-js";
import { toast } from "solid-sonner";

import { Alert, AlertDescription } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import { Skeleton } from "../components/ui/skeleton";
import { ApiError, client, type OffenesBild } from "../lib/api";
import { queryKeys } from "../lib/queryKeys";

// The five belegbilder.erkennung_status values, in German. The list only ever
// sees unassigned images, so the month status badge from Monat does not apply.
const statusLabel: Record<string, string> = {
  keine: "Ohne Erkennung",
  ausstehend: "Wartet auf Erkennung",
  laeuft: "Erkennung läuft",
  fertig: "Erkannt",
  fehler: "Erkennung fehlgeschlagen",
};

const statusVariant: Record<
  string,
  "secondary" | "outline" | "warning" | "error"
> = {
  keine: "outline",
  ausstehend: "secondary",
  laeuft: "secondary",
  fertig: "secondary",
  fehler: "error",
};

// How many rows one page asks for. The server caps at 100 and defaults to 50;
// 25 keeps a page of thumbnails readable and the page buttons few.
const SEITE = 25;

function zeitpunktLabel(stamp: string): string {
  const date = new Date(stamp);
  if (Number.isNaN(date.getTime())) {
    return stamp;
  }
  return date.toLocaleString("de-DE", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    timeZone: "Europe/Berlin",
  });
}

export default function Unassigned() {
  const [seite, setSeite] = createSignal(0);
  const query = useQuery(() => ({
    queryKey: [...queryKeys.offeneBilder, seite()],
    queryFn: () =>
      client.offeneBilder({ limit: SEITE, offset: seite() * SEITE }),
    retry: false,
  }));
  const bilder = () => query.data?.bilder ?? [];
  const gesamt = () => query.data?.gesamt ?? 0;
  // gesamt counts the whole filter, not the page, so this is where every
  // image beyond the first page stays reachable.
  const seiten = createMemo(() => Math.max(1, Math.ceil(gesamt() / SEITE)));

  // Deleting rows can shrink gesamt under the page the user is standing on,
  // for instance after emptying the last page. Fetching that offset then
  // returns nothing and the screen would claim "keine offenen Bilder" while
  // the rows sit a page back, so the page number follows the total.
  createEffect(() => {
    // Only once a page has loaded: while the next page is in flight the query
    // holds no data and gesamt would read as 0, which would drag the user off
    // the page they just asked for.
    if (query.isPending) {
      return;
    }
    const letzte = seiten() - 1;
    if (seite() > letzte) {
      setSeite(letzte);
    }
  });

  // The TTL sentence comes from the server, not from a number baked into this
  // file: BELEGAPP_UNASSIGNED_IMAGE_TTL is operator-configurable, and copy that
  // claims 24 hours while the sweeper waits 72 is worse than no copy at all.
  const einleitung = () => {
    const stunden = query.data?.ttl_stunden ?? 0;
    if (stunden <= 0) {
      return "Hochgeladene Bilder ohne Beleg. Zum Prüfen öffnen oder löschen.";
    }
    const frist =
      stunden === 1 ? "nach einer Stunde" : `nach ${stunden} Stunden`;
    return `Hochgeladene Bilder ohne Beleg. Zum Prüfen öffnen oder löschen. Ohne Zuordnung werden sie ${frist} entfernt.`;
  };

  return (
    <section class="flex flex-col gap-6">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Offene Bilder</h1>
        <p class="text-sm text-muted-foreground">{einleitung()}</p>
      </div>
      <Show when={query.isPending}>
        <div class="flex flex-col gap-3" aria-hidden="true">
          <Skeleton height={96} radius={12} />
          <Skeleton height={96} radius={12} />
          <Skeleton height={96} radius={12} />
        </div>
      </Show>
      <Show when={query.isError}>
        <Alert>
          <AlertDescription>
            Die offenen Bilder konnten nicht geladen werden.
          </AlertDescription>
        </Alert>
      </Show>
      <Show when={query.isSuccess}>
        <Show
          when={bilder().length > 0}
          fallback={
            <Card class="rounded-xl">
              <CardContent class="p-4">
                <p class="font-medium">Keine offenen Bilder</p>
                <p class="text-sm text-muted-foreground">
                  Jedes hochgeladene Bild ist einem Beleg zugeordnet.
                </p>
              </CardContent>
            </Card>
          }
        >
          <ul class="flex flex-col gap-3">
            <For each={bilder()}>
              {(bild) => (
                <li>
                  <Eintrag bild={bild} />
                </li>
              )}
            </For>
          </ul>
          <Show when={gesamt() > SEITE}>
            <div class="flex items-center justify-between gap-3">
              <p class="text-sm text-muted-foreground" aria-live="polite">
                {bilder().length} von {gesamt()} Bildern
              </p>
              <div class="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={seite() === 0 || query.isFetching}
                  onClick={() => setSeite((n) => Math.max(0, n - 1))}
                >
                  Zurück
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={seite() + 1 >= seiten() || query.isFetching}
                  onClick={() => setSeite((n) => n + 1)}
                >
                  Weiter
                </Button>
              </div>
            </div>
          </Show>
        </Show>
      </Show>
    </section>
  );
}

function Eintrag(props: { bild: OffenesBild }) {
  const queryClient = useQueryClient();
  const [busy, setBusy] = createSignal(false);
  const entfernen = async () => {
    if (busy()) {
      return;
    }
    setBusy(true);
    try {
      await client.deleteBild(props.bild.id);
      // The row is gone and the count with it, so the whole key refetches
      // rather than just the cached page.
      await queryClient.invalidateQueries({
        queryKey: queryKeys.offeneBilder,
      });
      toast.success("Bild gelöscht");
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : "Löschen fehlgeschlagen",
      );
    } finally {
      setBusy(false);
    }
  };
  // An image released from a deleted Beleg keeps its original upload time,
  // which reads oddly beside a TTL warning. The deadline is what explains the
  // row: overdue, and the next sweep takes it.
  const hochgeladen = () =>
    `Hochgeladen am ${zeitpunktLabel(props.bild.erstellt_am)}${
      props.bild.faellig_am ? ` · ${fristText(props.bild.faellig_am)}` : ""
    }`;

  return (
    <Card class="rounded-xl">
      <CardContent class="flex items-center gap-4 p-4">
        {/* Fixed portrait frame. object-contain keeps the whole receipt visible. */}
        <img
          alt="Beleg"
          class="h-24 w-16 shrink-0 rounded-lg border bg-muted object-contain"
          src={props.bild.thumbnail_url}
        />
        <div class="flex min-w-0 flex-1 flex-col gap-1.5">
          <Badge
            variant={statusVariant[props.bild.erkennung_status] ?? "outline"}
          >
            {statusLabel[props.bild.erkennung_status] ??
              props.bild.erkennung_status}
          </Badge>
          <p class="text-sm text-muted-foreground">{hochgeladen()}</p>
          <Show when={props.bild.erkennung_fehler}>
            {(text) => <p class="text-sm">{text()}</p>}
          </Show>
        </div>
        {/* Same deep link the capture flow uses: the review form reads the
            bild query parameter and loads the image through GET /belegbilder/{id}. */}
        <A
          class="shrink-0 rounded-lg border px-3 py-2 text-sm font-medium hover:bg-accent"
          href={`/belege/neu?bild=${props.bild.id}`}
        >
          Prüfen
        </A>
        {/* DELETE /belegbilder/{id} already exists and is session-guarded; it
            refuses an assigned image with 422. */}
        <Button
          variant="destructive"
          size="sm"
          disabled={busy()}
          onClick={() => void entfernen()}
        >
          Löschen
        </Button>
      </CardContent>
    </Card>
  );
}

// fristText renders the sweeper deadline the server computed. A deadline that
// already went by is the normal case for a row that came back from a deleted
// Beleg, so it gets its own wording instead of a date in the past read as a
// promise.
function fristText(faellig: string): string {
  const bis = new Date(faellig).getTime();
  if (Number.isNaN(bis)) {
    return `Entfernung am ${zeitpunktLabel(faellig)}`;
  }
  if (bis <= Date.now()) {
    return "Entfernung überfällig, der nächste Durchlauf löscht das Bild";
  }
  return `Entfernung am ${zeitpunktLabel(faellig)}`;
}
