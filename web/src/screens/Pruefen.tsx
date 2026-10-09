import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createSignal, onCleanup, Show } from "solid-js";
import { toast } from "solid-sonner";
import { CaptureInputs } from "../components/CaptureInputs";
import { ApiError, client, type Erkennung } from "../lib/api";
import { todayISO } from "../lib/dates";
import { downscale } from "../lib/image";
import { formatCentInput } from "../lib/money";
import { queryKeys } from "../lib/queryKeys";
import { type Draft, PruefenForm } from "./PruefenForm";

export default function Pruefen() {
  const params = useParams();
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [uploaded, setUploaded] = createSignal<string[]>([]);
  const [attempt, setAttempt] = createSignal(0);
  const [gaveUp, setGaveUp] = createSignal(false);
  const datumParam = () => {
    const raw = String(search.datum ?? "");
    return /^\d{4}-\d{2}-\d{2}$/.test(raw) ? raw : todayISO();
  };
  const draftJahr = () =>
    Number(datumParam().slice(0, 4)) || new Date().getFullYear();
  const profil = useQuery(() => ({
    queryKey: queryKeys.einstellungen,
    queryFn: () => client.einstellungen(),
    enabled: !params.id,
  }));
  const regel = useQuery(() => ({
    queryKey: queryKeys.regel(draftJahr()),
    queryFn: () => client.regel(draftJahr()),
    enabled: !params.id,
    retry: false,
  }));
  const addFile = async (file: File) => {
    try {
      const blob = await downscale(file);
      const first = bildIds().length === 0;
      const bild = await client.upload(blob, first);
      setUploaded((ids) => [...ids, bild.id].slice(0, 3));
    } catch (err) {
      toast.error(
        err instanceof ApiError
          ? err.message
          : "Das Bild konnte nicht hochgeladen werden.",
      );
    }
  };
  const existing = useQuery(() => ({
    queryKey: queryKeys.beleg(params.id ?? ""),
    queryFn: () => client.beleg(params.id ?? ""),
    enabled: Boolean(params.id),
  }));
  const initial = (): Draft => {
    const beleg = existing.data;
    if (beleg) {
      return {
        datum: beleg.datum,
        mahlzeit: beleg.mahlzeit,
        bezugsort: beleg.bezugsort,
        arbeitsort: beleg.arbeitsort,
        haendler_name: beleg.haendler_name,
        haendler_ort: beleg.haendler_ort,
        betrag: formatCentInput(beleg.belegbetrag_cent),
        korrigiert:
          beleg.korrigierter_betrag_cent == null
            ? ""
            : formatCentInput(beleg.korrigierter_betrag_cent),
        korrektur_grund: beleg.korrektur_grund ?? "",
        notiz: beleg.notiz,
      };
    }
    return {
      datum: datumParam(),
      mahlzeit: regel.data?.standard_mahlzeit ?? "mittag",
      bezugsort: profil.data?.standard_bezugsort ?? "supermarkt",
      arbeitsort: profil.data?.standard_arbeitsort ?? "betrieb",
      haendler_name: "",
      haendler_ort: "",
      betrag: "",
      korrigiert: "",
      korrektur_grund: "",
      notiz: "",
    };
  };
  const bildIds = () => {
    const fromBeleg = existing.data?.bilder.map((bild) => bild.id);
    if (fromBeleg) {
      return fromBeleg;
    }
    const fromQuery = search.bild ? [String(search.bild)] : [];
    return [...fromQuery, ...uploaded()].slice(0, 3);
  };
  const firstId = () => bildIds()[0] ?? "";
  createEffect(() => {
    firstId();
    attempt();
    setGaveUp(false);
    const timer = setTimeout(() => setGaveUp(true), 90_000);
    onCleanup(() => clearTimeout(timer));
  });
  const bild = useQuery(() => ({
    queryKey: queryKeys.bild(firstId()),
    queryFn: () => client.bild(firstId()),
    enabled: Boolean(firstId()),
    refetchInterval: (query) => {
      if (gaveUp()) {
        return false;
      }
      const status = query.state.data?.erkennung.status;
      if (!status || status === "ausstehend" || status === "laeuft") {
        return 1000;
      }
      return false;
    },
  }));
  const erkennung = (): Erkennung | undefined =>
    bild.data?.erkennung ?? existing.data?.bilder[0]?.erkennung;
  const retry = async () => {
    const id = firstId();
    if (!id) {
      return;
    }
    try {
      await client.erneut(id);
      setAttempt((n) => n + 1);
      await queryClient.invalidateQueries({ queryKey: queryKeys.bild(id) });
    } catch (err) {
      toast.error(
        err instanceof ApiError
          ? err.message
          : "Erkennung konnte nicht gestartet werden.",
      );
    }
  };

  return (
    <section class="flex flex-col gap-4">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">
          {params.id ? "Beleg prüfen" : "Beleg erfassen"}
        </h1>
        <p class="text-sm text-muted-foreground">
          Betrag, Händler und Erstattung prüfen, dann speichern.
        </p>
      </div>
      <Show when={!params.id && bildIds().length < 3}>
        <CaptureInputs onFile={(file) => void addFile(file)} />
      </Show>
      <Show when={bildIds()[0]}>
        <img
          alt="Belegbild"
          class="max-h-72 w-full rounded-xl border bg-muted object-contain"
          src={`/api/v1/belegbilder/${bildIds()[0]}/datei`}
        />
      </Show>
      <Show when={params.id && existing.isPending}>
        <p class="text-sm text-muted-foreground">Lädt …</p>
      </Show>
      <Show when={params.id && existing.isError}>
        <p class="rounded-xl border border-warning/40 bg-warning px-3 py-2 text-sm text-warning-foreground">
          Der Beleg konnte nicht geladen werden.
        </p>
      </Show>
      <Show when={!params.id && profil.isPending}>
        <p class="text-sm text-muted-foreground">Lädt …</p>
      </Show>
      <Show when={!params.id && profil.isError}>
        <p class="rounded-xl border border-warning/40 bg-warning px-3 py-2 text-sm text-warning-foreground">
          Die Einstellungen konnten nicht geladen werden.
        </p>
      </Show>
      <Show when={Boolean(params.id ? existing.data : profil.data)}>
        <PruefenForm
          initial={initial()}
          bildIds={bildIds()}
          beleg={existing.data}
          erkennung={erkennung()}
          timedOut={
            gaveUp() &&
            (erkennung()?.status === "ausstehend" ||
              erkennung()?.status === "laeuft")
          }
          onRetry={firstId() ? () => void retry() : undefined}
          onDone={() => {
            if (params.id) {
              void queryClient.invalidateQueries({
                queryKey: queryKeys.beleg(params.id),
              });
            }
            void queryClient.invalidateQueries({
              queryKey: queryKeys.monatAll,
            });
            navigate("/monat");
          }}
        />
      </Show>
    </section>
  );
}
