import { A, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import { createSignal, Show } from "solid-js";

import { CaptureInputs, openCaptured } from "../components/CaptureInputs";
import { Alert, AlertDescription } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "../components/ui/dialog";
import { Skeleton } from "../components/ui/skeleton";
import { ApiError, type Beleg, client } from "../lib/api";
import { currentMonth, todayISO } from "../lib/dates";
import { formatCent } from "../lib/money";
import { queryKeys } from "../lib/queryKeys";

export default function Heute() {
  const navigate = useNavigate();
  const today = todayISO();
  const month = currentMonth();
  const monat = useQuery(() => ({
    queryKey: queryKeys.monat(month),
    queryFn: () => client.monat(month),
    retry: false,
  }));

  const yearMissing = () =>
    monat.error instanceof ApiError &&
    monat.error.code === "E_JAHRESREGEL_FEHLT";
  const beleg = () => monat.data?.belege.find((item) => item.datum === today);
  const limit = () => monat.data?.jahresregel?.monatslimit ?? 15;
  const summen = () => monat.data?.summen;
  const pauschal = () => Boolean(monat.data?.jahresregel?.pauschalierung);

  return (
    <section class="flex flex-col gap-6">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Heute</h1>
        <p class="text-sm text-muted-foreground">
          Beleg des Tages und die Summen des laufenden Monats.
        </p>
      </div>
      <Show when={monat.isPending}>
        <div class="flex flex-col gap-3">
          <Skeleton height={176} radius={12} />
          <div class="grid gap-3 sm:grid-cols-3">
            <Skeleton height={104} radius={12} />
            <Skeleton height={104} radius={12} />
            <Skeleton height={104} radius={12} />
          </div>
        </div>
      </Show>
      <Show when={monat.isError}>
        <Alert>
          <AlertDescription>
            <Show
              when={yearMissing()}
              fallback={<>Der Monat konnte nicht geladen werden.</>}
            >
              Für dieses Jahr fehlt die Jahresregel.{" "}
              <A
                class="font-medium underline underline-offset-4"
                href={`/einstellungen/jahre/${today.slice(0, 4)}`}
              >
                Regel anlegen
              </A>
            </Show>
          </AlertDescription>
        </Alert>
      </Show>
      <Show when={monat.isSuccess}>
        <Show
          when={beleg()}
          fallback={
            <Card class="rounded-xl">
              <CardHeader>
                <CardTitle>Noch kein Beleg</CardTitle>
                <CardDescription>
                  Für heute liegt noch kein Beleg vor.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <CaptureInputs
                  onFile={(file) => void openCaptured(navigate, file)}
                />
              </CardContent>
            </Card>
          }
        >
          {(item) => <Vorhanden item={item()} />}
        </Show>
        <div class="grid gap-3 sm:grid-cols-3">
          <Stat
            label="Belegtage"
            hint="Monat"
            value={`${summen()?.anzahl ?? 0} / ${limit()}`}
          />
          <Stat
            label="Erstattung Σ"
            hint="Monat"
            value={formatCent(summen()?.erstattung_cent ?? 0)}
          />
          <Card class="rounded-xl">
            <CardHeader class="p-4 pb-2">
              <CardDescription>Steuerfrei / Pauschal</CardDescription>
              <CardTitle class="text-2xl tabular-nums">
                {formatCent(summen()?.steuerfrei_cent ?? 0)}
              </CardTitle>
            </CardHeader>
            <CardContent class="p-4 pt-0">
              <Badge variant={pauschal() ? "secondary" : "outline"}>
                {pauschal()
                  ? `Pauschal ${formatCent(summen()?.pauschal_gesamt_cent ?? 0)}`
                  : "ohne Pauschalierung"}
              </Badge>
            </CardContent>
          </Card>
        </div>
      </Show>
    </section>
  );
}

function Stat(props: { label: string; hint: string; value: string }) {
  return (
    <Card class="rounded-xl">
      <CardHeader class="p-4">
        <CardDescription>{props.label}</CardDescription>
        <CardTitle class="text-2xl tabular-nums">{props.value}</CardTitle>
        <p class="text-xs text-muted-foreground">{props.hint}</p>
      </CardHeader>
    </Card>
  );
}

function Vorhanden(props: { item: Beleg }) {
  const [offen, setOffen] = createSignal(false);
  const bild = () => props.item.bilder[0];
  return (
    <Card class="rounded-xl">
      <CardContent class="flex items-center gap-4 p-4">
        <A href={`/belege/${props.item.id}`} class="min-w-0 flex-1">
          <p class="text-sm text-muted-foreground">Beleg vorhanden</p>
          <p class="text-2xl font-semibold tabular-nums">
            {formatCent(props.item.belegbetrag_cent)}
          </p>
          <p class="text-sm">
            Erstattung {formatCent(props.item.berechnung.erstattung_cent)}
          </p>
        </A>
        <Show when={bild()}>
          {(row) => (
            <>
              <button
                type="button"
                class="shrink-0 cursor-pointer rounded-xl border bg-muted p-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background"
                aria-label="Belegbild öffnen"
                onClick={() => setOffen(true)}
              >
                {/* Fixed portrait frame. object-contain keeps the whole receipt visible. */}
                <img
                  alt="Beleg"
                  class="h-40 w-28 object-contain"
                  src={row().thumbnail_url}
                />
              </button>
              <Dialog open={offen()} onOpenChange={setOffen}>
                <DialogContent>
                  <DialogHeader>
                    <DialogTitle>Belegbild</DialogTitle>
                  </DialogHeader>
                  <img
                    alt="Belegbild"
                    class="max-h-[65vh] w-full rounded-xl bg-muted object-contain"
                    src={row().url}
                  />
                </DialogContent>
              </Dialog>
            </>
          )}
        </Show>
      </CardContent>
    </Card>
  );
}
