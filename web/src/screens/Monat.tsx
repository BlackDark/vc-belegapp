import { Dialog } from "@kobalte/core/dialog";
import { A } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createSignal, For, Show } from "solid-js";
import { Button } from "../components/ui";
import { ApiError, client, type Monat as MonatData } from "../lib/api";
import { currentMonth } from "../lib/dates";
import { formatCent } from "../lib/money";
import { queryKeys } from "../lib/queryKeys";

const statusLabel: Record<string, string> = {
  offen: "Offen",
  gesperrt: "Gesperrt",
  geaendert: "Geändert",
};

const erklaerungText =
  "Ich versichere, dass jeder aufgeführte Beleg eine Mahlzeit betrifft, die ich an dem angegebenen Tag als Arbeitstag (kein Urlaub, keine Krankheit, keine Auswärtstätigkeit) selbst erworben habe und die zum Verzehr an diesem Tag bestimmt war. Nicht erstattungsfähige Artikel (z. B. Alkohol, Tabak, Pfand, Non-Food, Vorratskäufe) habe ich herausgerechnet. Jeder Beleg wird nur einmal eingereicht.";

function mark(ergebnis: string) {
  if (ergebnis === "fehler") return "✗";
  if (ergebnis === "warnung") return "⚠";
  return "✓";
}

export default function Monat() {
  const [monat, setMonat] = createSignal(currentMonth());
  const [open, setOpen] = createSignal(false);
  const queryClient = useQueryClient();
  const query = useQuery(() => ({
    queryKey: queryKeys.monat(monat()),
    queryFn: () => client.monat(monat()),
    retry: false,
  }));

  return (
    <section class="flex flex-col gap-4">
      <div class="flex items-center justify-between gap-3">
        <h1 class="text-2xl font-semibold">Monat</h1>
        <input
          aria-label="Monat"
          type="month"
          class="min-h-12 rounded-xl border border-zinc-300 bg-white px-3 dark:border-zinc-700 dark:bg-zinc-900"
          value={monat()}
          onInput={(event) => setMonat(event.currentTarget.value)}
        />
      </div>
      <Show when={query.isPending}>
        <p>Lädt …</p>
      </Show>
      <Show when={query.isError}>
        <p class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
          Der Monat konnte nicht geladen werden.
        </p>
      </Show>
      <Show when={query.data}>
        {(data) => (
          <>
            <p class="w-fit rounded-full bg-zinc-200 px-3 py-1 text-sm dark:bg-zinc-800">
              Status {statusLabel[data().status] ?? data().status}
            </p>
            <Show when={data().status === "gesperrt"}>
              <p class="rounded-xl bg-zinc-100 px-3 py-2 text-sm dark:bg-zinc-900">
                Dieser Monat ist gesperrt. Änderungen brauchen einen
                Änderungsgrund.
              </p>
            </Show>
            <Show when={data().status === "geaendert"}>
              <p class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
                Nach dem letzten Export geändert. Ein neuer Export erzeugt
                Version {data().letzte_exportversion + 1}.
              </p>
            </Show>
            <article class="rounded-2xl bg-zinc-100 p-4 dark:bg-zinc-900">
              <p>Belege {data().summen.anzahl}</p>
              <p>Erstattung {formatCent(data().summen.erstattung_cent)}</p>
              <p>Eigenanteil {formatCent(data().summen.eigenanteil_cent)}</p>
              <p>AG-Kosten {formatCent(data().summen.ag_kosten_cent)}</p>
            </article>
            <div class="grid grid-cols-7 gap-1 text-center text-xs">
              <For each={["Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"]}>
                {(day) => <span class="text-zinc-500">{day}</span>}
              </For>
              <For
                each={Array.from({
                  length: (data().tage[0]?.wochentag ?? 1) - 1,
                })}
              >
                {() => <span />}
              </For>
              <For each={data().tage}>
                {(tag) => (
                  <A
                    href={
                      tag.beleg_id
                        ? `/belege/${tag.beleg_id}`
                        : `/belege/neu?datum=${tag.datum}`
                    }
                    class={`rounded-lg px-1 py-2 ${tag.wochenende || tag.feiertag ? "bg-amber-100 dark:bg-amber-950" : "bg-zinc-100 dark:bg-zinc-900"} ${tag.beleg_id ? "font-semibold" : ""}`}
                  >
                    {Number(tag.datum.slice(8))}
                  </A>
                )}
              </For>
            </div>
            <ul class="flex flex-col gap-2">
              <For each={data().warnungen}>
                {(warn) => (
                  <li class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
                    {warn.text}
                  </li>
                )}
              </For>
            </ul>
            <ul class="flex flex-col gap-2">
              <For each={data().belege}>
                {(beleg) => (
                  <li>
                    <A
                      href={`/belege/${beleg.id}`}
                      class="flex items-center justify-between rounded-xl border border-zinc-200 px-3 py-3 dark:border-zinc-800"
                    >
                      <span>
                        {beleg.datum.slice(8, 10)}.{beleg.datum.slice(5, 7)}.{" "}
                        {beleg.haendler_name}
                      </span>
                      <span>
                        {formatCent(beleg.berechnung.erstattung_cent)}
                      </span>
                    </A>
                  </li>
                )}
              </For>
            </ul>
            <Button onClick={() => setOpen(true)}>Exportieren</Button>
            <Show when={data().exporte.length > 0}>
              <h2 class="text-lg font-semibold">Exportversionen</h2>
              <ul class="flex flex-col gap-2">
                <For each={data().exporte}>
                  {(exp) => (
                    <li class="flex flex-wrap items-center gap-3 rounded-xl border border-zinc-200 px-3 py-2 dark:border-zinc-800">
                      <span>Version {exp.version}</span>
                      <Show when={exp.aufbewahrung_bis}>
                        <span class="text-sm text-zinc-600 dark:text-zinc-300">
                          {exp.aufbewahrung_abgelaufen
                            ? `Aufbewahrungsfrist abgelaufen (${exp.aufbewahrung_bis.slice(0, 10)}). Keine automatische Löschung.`
                            : `Aufbewahrung bis ${exp.aufbewahrung_bis.slice(0, 10)}`}
                        </span>
                      </Show>
                      <a
                        class="underline"
                        rel="external"
                        href={`/api/v1/exporte/${exp.id}/pdf`}
                      >
                        PDF
                      </a>
                      <Show when={exp.csv}>
                        <a
                          class="underline"
                          rel="external"
                          href={`/api/v1/exporte/${exp.id}/csv`}
                        >
                          CSV
                        </a>
                      </Show>
                      <Show when={exp.zip}>
                        <a
                          class="underline"
                          rel="external"
                          href={`/api/v1/exporte/${exp.id}/zip`}
                        >
                          ZIP
                        </a>
                      </Show>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
            <ExportDialog
              open={open()}
              monat={monat()}
              data={data()}
              onOpenChange={setOpen}
              onDone={() =>
                queryClient.invalidateQueries({
                  queryKey: queryKeys.monat(monat()),
                })
              }
            />
          </>
        )}
      </Show>
    </section>
  );
}

function ExportDialog(props: {
  open: boolean;
  monat: string;
  data: MonatData;
  onOpenChange: (open: boolean) => void;
  onDone: () => Promise<void> | void;
}) {
  const [erklaerung, setErklaerung] = createSignal(false);
  const [warnungen, setWarnungen] = createSignal(false);
  const [csv, setCsv] = createSignal(false);
  const [zip, setZip] = createSignal(false);
  const [seeded, setSeeded] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal("");
  const [strict, setStrict] = createSignal<MonatData["pruefpunkte"] | null>(
    null,
  );
  const settings = useQuery(() => ({
    queryKey: queryKeys.einstellungen,
    queryFn: () => client.einstellungen(),
    enabled: props.open,
  }));
  createEffect(() => {
    const row = settings.data;
    if (row && props.open && !seeded()) {
      setCsv(row.export_csv_standard);
      setZip(row.export_zip_standard);
      setSeeded(true);
    }
    if (!props.open) {
      setSeeded(false);
      setErklaerung(false);
      setWarnungen(false);
      setError("");
      setStrict(null);
    }
  });
  const checks = () => strict() ?? props.data.pruefpunkte;
  const blocking = () => checks().some((item) => item.ergebnis === "fehler");
  const needWarn = () =>
    props.data.warnungen.length > 0 ||
    props.data.pruefpunkte.some((item) => item.ergebnis === "warnung");
  const finalDisabled = () =>
    busy() || blocking() || !erklaerung() || (needWarn() && !warnungen());

  async function preview() {
    setBusy(true);
    setError("");
    try {
      const blob = await client.previewMonat(props.monat);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `nachweis-${props.monat}-entwurf.pdf`;
      link.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Vorschau fehlgeschlagen",
      );
    } finally {
      setBusy(false);
    }
  }

  async function finish() {
    setBusy(true);
    setError("");
    try {
      await client.exportMonat(props.monat, {
        erklaerung_bestaetigt: true,
        warnungen_bestaetigt: warnungen(),
        csv: csv(),
        zip: zip(),
      });
      props.onOpenChange(false);
      await props.onDone();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Export fehlgeschlagen");
      if (
        err instanceof ApiError &&
        err.code === "E_PRUEFPUNKT_FEHLGESCHLAGEN"
      ) {
        setStrict([]);
        try {
          const fresh = await client.monatPruefpunkte(props.monat);
          setStrict(fresh.pruefpunkte);
        } catch {
          setStrict([]);
        }
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 bg-black/40" />
        <Dialog.Content class="fixed inset-x-4 top-8 z-20 max-h-[85vh] overflow-y-auto rounded-2xl bg-white p-4 shadow-xl dark:bg-zinc-900">
          <Dialog.Title class="text-lg font-semibold">
            Monatsexport
          </Dialog.Title>
          <h2 class="mt-3 font-medium">Prüfpunkte</h2>
          <ul class="mt-1 flex flex-col gap-1 text-sm">
            <For each={checks()}>
              {(item) => (
                <li>
                  {mark(item.ergebnis)} {item.text}
                </li>
              )}
            </For>
          </ul>
          <Show when={blocking()}>
            <p class="mt-2 text-sm text-red-700 dark:text-red-300">
              Finaler Export ist blockiert, bis die mit ✗ markierten Prüfpunkte
              behoben sind.
            </p>
          </Show>
          <Show when={props.data.warnungen.length > 0}>
            <h2 class="mt-3 font-medium">Warnungen</h2>
            <ul class="mt-1 flex flex-col gap-1 text-sm">
              <For each={props.data.warnungen}>
                {(warn) => <li>{warn.text}</li>}
              </For>
            </ul>
          </Show>
          <label class="mt-3 flex items-center gap-3">
            <input
              type="checkbox"
              class="size-5"
              checked={csv()}
              onChange={(event) => setCsv(event.currentTarget.checked)}
            />
            CSV
          </label>
          <label class="mt-2 flex items-center gap-3">
            <input
              type="checkbox"
              class="size-5"
              checked={zip()}
              onChange={(event) => setZip(event.currentTarget.checked)}
            />
            ZIP mit Originalbildern
          </label>
          <label class="mt-3 flex items-start gap-3 text-sm">
            <input
              type="checkbox"
              class="mt-1 size-5 shrink-0"
              checked={erklaerung()}
              onChange={(event) => setErklaerung(event.currentTarget.checked)}
            />
            <span>{erklaerungText}</span>
          </label>
          <label class="mt-3 flex items-center gap-3">
            <input
              type="checkbox"
              class="size-5"
              checked={warnungen()}
              onChange={(event) => setWarnungen(event.currentTarget.checked)}
            />
            Warnungen geprüft
          </label>
          <Show when={error()}>
            <p class="mt-2 text-sm text-red-700 dark:text-red-300">{error()}</p>
          </Show>
          <div class="mt-4 flex flex-col gap-2">
            <Button disabled={busy()} onClick={() => void preview()}>
              Vorschau
            </Button>
            <Button disabled={finalDisabled()} onClick={() => void finish()}>
              Final exportieren
            </Button>
            <Button
              class="bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
              onClick={() => props.onOpenChange(false)}
            >
              Abbrechen
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog>
  );
}
