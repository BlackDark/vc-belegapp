import { A } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createSignal, For, Show } from "solid-js";
import { Alert, AlertDescription } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Calendar } from "../components/ui/calendar";
import { Card, CardContent } from "../components/ui/card";
import { Checkbox } from "../components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "../components/ui/dialog";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Separator } from "../components/ui/separator";
import { Skeleton } from "../components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../components/ui/table";
import { mahlzeitLabel } from "../lib/amtlich";
import { ApiError, client, type Monat as MonatData } from "../lib/api";
import { currentMonth, todayISO } from "../lib/dates";
import { formatCent } from "../lib/money";
import { queryKeys } from "../lib/queryKeys";

const statusLabel: Record<string, string> = {
  offen: "Offen",
  gesperrt: "Gesperrt",
  geaendert: "Geändert",
};

const statusVariant: Record<string, "secondary" | "outline" | "warning"> = {
  offen: "secondary",
  gesperrt: "outline",
  geaendert: "warning",
};

const erklaerungText =
  "Ich versichere, dass jeder aufgeführte Beleg eine Mahlzeit betrifft, die ich an dem angegebenen Tag als Arbeitstag (kein Urlaub, keine Krankheit, keine Auswärtstätigkeit) selbst erworben habe und die zum Verzehr an diesem Tag bestimmt war. Nicht erstattungsfähige Artikel (z. B. Alkohol, Tabak, Pfand, Non-Food, Vorratskäufe) habe ich herausgerechnet. Jeder Beleg wird nur einmal eingereicht.";

function mark(ergebnis: string): { symbol: string; label: string } {
  if (ergebnis === "fehler") return { symbol: "✗", label: "Fehler" };
  if (ergebnis === "warnung") return { symbol: "⚠", label: "Warnung" };
  return { symbol: "✓", label: "Bestanden" };
}

function monthCaption(value: string) {
  const date = new Date(`${value}-01T12:00:00`);
  return date.toLocaleDateString("de-DE", { month: "long", year: "numeric" });
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
  const today = todayISO();

  return (
    <section class="flex flex-col gap-6">
      <div class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 class="text-2xl font-semibold tracking-tight">Monat</h1>
          <p class="text-sm text-muted-foreground">
            Kalender, Belege und Monatsexport.
          </p>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="monat-input">Monat</Label>
          <Input
            id="monat-input"
            aria-label="Monat"
            type="month"
            class="w-44"
            value={monat()}
            onInput={(event) => setMonat(event.currentTarget.value)}
          />
        </div>
      </div>
      <Show when={query.isPending}>
        <div class="flex flex-col gap-3">
          <Skeleton height={88} radius={12} />
          <Skeleton height={280} radius={12} />
          <Skeleton height={180} radius={12} />
        </div>
      </Show>
      <Show when={query.isError}>
        <Alert>
          <AlertDescription>
            Der Monat konnte nicht geladen werden.
          </AlertDescription>
        </Alert>
      </Show>
      <Show when={query.data}>
        {(data) => (
          <>
            <div class="flex flex-wrap items-center gap-2">
              <Badge variant={statusVariant[data().status] ?? "secondary"}>
                Status {statusLabel[data().status] ?? data().status}
              </Badge>
            </div>
            <Show when={data().status === "gesperrt"}>
              <Alert>
                <AlertDescription>
                  Dieser Monat ist gesperrt. Änderungen brauchen einen
                  Änderungsgrund.
                </AlertDescription>
              </Alert>
            </Show>
            <Show when={data().status === "geaendert"}>
              <Alert>
                <AlertDescription>
                  Nach dem letzten Export geändert. Ein neuer Export erzeugt
                  Version {data().letzte_exportversion + 1}.
                </AlertDescription>
              </Alert>
            </Show>
            <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <Sum label="Belege" value={String(data().summen.anzahl)} />
              <Sum
                label="Monat"
                value={`Erstattung ${formatCent(data().summen.erstattung_cent)}`}
              />
              <Sum
                label="Anteil"
                value={`Eigenanteil ${formatCent(data().summen.eigenanteil_cent)}`}
              />
              <Sum
                label="Arbeitgeber"
                value={`AG-Kosten ${formatCent(data().summen.ag_kosten_cent)}`}
              />
            </div>
            <Calendar
              caption={monthCaption(monat())}
              weekdays={["Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"]}
              lead={(data().tage[0]?.wochentag ?? 1) - 1}
              days={data().tage.map((tag) => ({
                key: tag.datum,
                label: String(Number(tag.datum.slice(8))),
                href: tag.beleg_id
                  ? `/belege/${tag.beleg_id}`
                  : `/belege/neu?datum=${tag.datum}`,
                muted: tag.wochenende || Boolean(tag.feiertag),
                active: Boolean(tag.beleg_id),
                today: tag.datum === today,
              }))}
            />
            <Show when={data().warnungen.length > 0}>
              <ul class="flex flex-col gap-2">
                <For each={data().warnungen}>
                  {(warn) => (
                    <li>
                      <Alert>
                        <AlertDescription>{warn.text}</AlertDescription>
                      </Alert>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
            <Card class="rounded-xl">
              <CardContent class="p-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Beleg</TableHead>
                      <TableHead>Mahlzeit</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead class="text-right">Erstattung</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    <Show
                      when={data().belege.length > 0}
                      fallback={
                        <TableRow>
                          <TableCell
                            colspan={4}
                            class="h-24 text-center text-muted-foreground"
                          >
                            Keine Belege in diesem Monat.
                          </TableCell>
                        </TableRow>
                      }
                    >
                      <For each={data().belege}>
                        {(beleg) => (
                          <TableRow>
                            <TableCell>
                              <A
                                href={`/belege/${beleg.id}`}
                                class="font-medium underline-offset-4 hover:underline"
                              >
                                {`${beleg.datum.slice(8, 10)}.${beleg.datum.slice(5, 7)}. ${beleg.haendler_name}`}
                              </A>
                            </TableCell>
                            <TableCell>
                              <Badge variant="secondary">
                                {mahlzeitLabel[beleg.mahlzeit] ??
                                  beleg.mahlzeit}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              <Badge variant="outline">
                                {statusLabel[beleg.monat_status] ??
                                  beleg.monat_status}
                              </Badge>
                            </TableCell>
                            <TableCell class="text-right tabular-nums">
                              {formatCent(beleg.berechnung.erstattung_cent)}
                            </TableCell>
                          </TableRow>
                        )}
                      </For>
                    </Show>
                  </TableBody>
                </Table>
              </CardContent>
            </Card>
            <Button onClick={() => setOpen(true)}>Exportieren</Button>
            <Show when={data().exporte.length > 0}>
              <div class="flex flex-col gap-3">
                <h2 class="text-lg font-semibold">Exportversionen</h2>
                <ul class="flex flex-col gap-2">
                  <For each={data().exporte}>
                    {(exp) => (
                      <li class="flex flex-wrap items-center gap-3 rounded-xl border px-3 py-2">
                        <span class="font-medium">Version {exp.version}</span>
                        <Show when={exp.aufbewahrung_bis}>
                          <span class="text-sm text-muted-foreground">
                            {exp.aufbewahrung_abgelaufen
                              ? `Aufbewahrungsfrist abgelaufen (${exp.aufbewahrung_bis.slice(0, 10)}). Keine automatische Löschung.`
                              : `Aufbewahrung bis ${exp.aufbewahrung_bis.slice(0, 10)}`}
                          </span>
                        </Show>
                        <a
                          class="text-sm font-medium underline underline-offset-4"
                          rel="external"
                          href={`/api/v1/exporte/${exp.id}/pdf`}
                        >
                          PDF
                        </a>
                        <Show when={exp.csv}>
                          <a
                            class="text-sm font-medium underline underline-offset-4"
                            rel="external"
                            href={`/api/v1/exporte/${exp.id}/csv`}
                          >
                            CSV
                          </a>
                        </Show>
                        <Show when={exp.zip}>
                          <a
                            class="text-sm font-medium underline underline-offset-4"
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
              </div>
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

function Sum(props: { label: string; value: string }) {
  return (
    <Card class="rounded-xl">
      <CardContent class="p-4">
        <p class="text-sm text-muted-foreground">{props.label}</p>
        <p class="text-xl font-semibold tabular-nums">{props.value}</p>
      </CardContent>
    </Card>
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
      <DialogContent class="max-h-[85vh]">
        <DialogHeader>
          <DialogTitle>Monatsexport</DialogTitle>
          <DialogDescription>
            Prüfpunkte bestätigen und den Nachweis erzeugen.
          </DialogDescription>
        </DialogHeader>
        <h2 class="font-medium">Prüfpunkte</h2>
        <ul class="flex flex-col gap-1 text-sm">
          <For each={checks()}>
            {(item) => {
              const status = mark(item.ergebnis);
              return (
                <li>
                  <span aria-hidden="true">{status.symbol}</span> {item.text}
                  <span class="sr-only"> ({status.label})</span>
                </li>
              );
            }}
          </For>
        </ul>
        <Show when={blocking()}>
          <p class="text-sm text-destructive">
            Finaler Export ist blockiert, bis die mit ✗ markierten Prüfpunkte
            behoben sind.
          </p>
        </Show>
        <Show when={props.data.warnungen.length > 0}>
          <h2 class="font-medium">Warnungen</h2>
          <ul class="flex flex-col gap-1 text-sm">
            <For each={props.data.warnungen}>
              {(warn) => <li>{warn.text}</li>}
            </For>
          </ul>
        </Show>
        <Separator />
        <div class="flex flex-col gap-3">
          <Checkbox checked={csv()} onChange={setCsv}>
            CSV
          </Checkbox>
          <Checkbox checked={zip()} onChange={setZip}>
            ZIP mit Originalbildern
          </Checkbox>
          <Checkbox checked={erklaerung()} onChange={setErklaerung}>
            {erklaerungText}
          </Checkbox>
          <Checkbox checked={warnungen()} onChange={setWarnungen}>
            Warnungen geprüft
          </Checkbox>
        </div>
        <Show when={error()}>
          <p class="text-sm text-destructive">{error()}</p>
        </Show>
        <DialogFooter>
          <Button variant="outline" onClick={() => props.onOpenChange(false)}>
            Abbrechen
          </Button>
          <Button
            variant="secondary"
            disabled={busy()}
            onClick={() => void preview()}
          >
            Vorschau
          </Button>
          <Button disabled={finalDisabled()} onClick={() => void finish()}>
            Final exportieren
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
