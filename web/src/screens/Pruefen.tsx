import { ToggleGroup } from "@kobalte/core/toggle-group";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createForm } from "@tanstack/solid-form";
import { useQuery } from "@tanstack/solid-query";
import { createEffect, createMemo, createSignal, For, Show } from "solid-js";
import { toast } from "solid-sonner";
import { Button, ReasonDialog, TextField } from "../components/ui";
import { bezugsorte, mahlzeitLabel } from "../lib/amtlich";
import { ApiError, type Beleg, client, type Warnung } from "../lib/api";
import { downscale } from "../lib/image";
import { formatCent, parseEuroToCent } from "../lib/money";
import { CaptureInputs } from "./Heute";

type Draft = {
  datum: string;
  mahlzeit: string;
  bezugsort: string;
  arbeitsort: string;
  haendler_name: string;
  haendler_ort: string;
  betrag: string;
  korrigiert: string;
  korrektur_grund: string;
  notiz: string;
};

export function PruefenForm(props: {
  initial: Draft;
  bildIds: string[];
  beleg?: Beleg;
  onDone: () => void;
}) {
  const [warnungen, setWarnungen] = createSignal<Warnung[]>([]);
  const [berechnung, setBerechnung] = createSignal<Beleg["berechnung"] | null>(
    null,
  );
  const [monatStatus, setMonatStatus] = createSignal(
    props.beleg?.monat_status ?? "offen",
  );
  const [reasonOpen, setReasonOpen] = createSignal(false);
  const [pending, setPending] = createSignal<Draft | null>(null);
  const jahr = createMemo(
    () => Number(props.initial.datum.slice(0, 4)) || new Date().getFullYear(),
  );
  const regel = useQuery(() => ({
    queryKey: ["regel", jahr()],
    queryFn: () => client.regel(jahr()),
    retry: false,
  }));

  const form = createForm(() => ({
    defaultValues: props.initial,
    onSubmit: async ({ value }) => {
      if (monatStatus() === "gesperrt" || monatStatus() === "geaendert") {
        setPending(value);
        setReasonOpen(true);
        return;
      }
      await save(value, undefined);
    },
  }));

  const payload = (value: Draft, grund?: string) => {
    const betrag = parseEuroToCent(value.betrag);
    const korr = value.korrigiert.trim()
      ? parseEuroToCent(value.korrigiert)
      : null;
    return {
      id: props.beleg?.id,
      datum: value.datum,
      mahlzeit: value.mahlzeit,
      bezugsort: value.bezugsort,
      arbeitsort: value.arbeitsort,
      haendler_name: value.haendler_name,
      haendler_ort: value.haendler_ort,
      belegbetrag_cent: betrag ?? 0,
      korrigierter_betrag_cent: korr,
      korrektur_grund: korr == null ? null : value.korrektur_grund,
      notiz: value.notiz,
      bild_ids: props.bildIds,
      version: props.beleg?.version,
      aenderungsgrund: grund,
    };
  };

  const refresh = async (value: Draft) => {
    if (
      !value.haendler_name ||
      !value.datum ||
      parseEuroToCent(value.betrag) == null
    ) {
      return;
    }
    try {
      const view = await client.preview(payload(value));
      setBerechnung(view.berechnung);
      setWarnungen(view.warnungen);
      if (view.monat_status) {
        setMonatStatus(view.monat_status);
      }
    } catch (err) {
      if (err instanceof ApiError) {
        setWarnungen(
          err.felder.map((feld) => ({ code: feld.code, text: feld.text })),
        );
      }
    }
  };

  const save = async (value: Draft, grund?: string) => {
    try {
      const body = payload(value, grund);
      if (props.beleg) {
        await client.patchBeleg(props.beleg.id, body);
      } else {
        await client.createBeleg(body);
      }
      toast.success("Beleg gespeichert");
      props.onDone();
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : "Speichern fehlgeschlagen",
      );
    }
  };

  const watch = form.useStore((state) =>
    [
      state.values.datum,
      state.values.mahlzeit,
      state.values.bezugsort,
      state.values.arbeitsort,
      state.values.haendler_name,
      state.values.haendler_ort,
      state.values.betrag,
      state.values.korrigiert,
      state.values.korrektur_grund,
    ].join("\n"),
  );
  createEffect(() => {
    watch();
    void refresh(form.state.values);
  });

  const meals = () => regel.data?.mahlzeiten ?? ["mittag"];

  return (
    <form
      class="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <form.Field name="datum">
        {(field) => (
          <TextField
            label="Datum"
            type="date"
            value={field().state.value}
            onChange={(value) => field().handleChange(value)}
          />
        )}
      </form.Field>
      <div>
        <p class="mb-1 text-sm font-medium">Mahlzeitart</p>
        <form.Field name="mahlzeit">
          {(field) => (
            <ToggleGroup
              value={field().state.value}
              onChange={(value) => value && field().handleChange(value)}
              class="grid grid-cols-3 gap-2"
            >
              <For each={meals()}>
                {(meal) => (
                  <ToggleGroup.Item
                    value={meal}
                    class="min-h-12 rounded-xl border border-zinc-300 data-[pressed]:bg-emerald-800 data-[pressed]:text-white dark:border-zinc-700"
                  >
                    {mahlzeitLabel[meal] ?? meal}
                  </ToggleGroup.Item>
                )}
              </For>
            </ToggleGroup>
          )}
        </form.Field>
      </div>
      <form.Field name="bezugsort">
        {(field) => (
          <label class="flex flex-col gap-1 text-sm font-medium">
            Bezugsort
            <select
              class="min-h-12 rounded-xl border border-zinc-300 bg-white px-3 dark:border-zinc-700 dark:bg-zinc-900"
              value={field().state.value}
              onChange={(event) =>
                field().handleChange(event.currentTarget.value)
              }
            >
              <For each={bezugsorte}>
                {(item) => <option value={item[0]}>{item[1]}</option>}
              </For>
            </select>
          </label>
        )}
      </form.Field>
      <form.Field name="arbeitsort">
        {(field) => (
          <div>
            <p class="mb-1 text-sm font-medium">Arbeitsort</p>
            <ToggleGroup
              value={field().state.value}
              onChange={(value) => value && field().handleChange(value)}
              class="grid grid-cols-2 gap-2"
            >
              <ToggleGroup.Item
                value="betrieb"
                class="min-h-12 rounded-xl border border-zinc-300 data-[pressed]:bg-emerald-800 data-[pressed]:text-white dark:border-zinc-700"
              >
                Betrieb
              </ToggleGroup.Item>
              <ToggleGroup.Item
                value="homeoffice"
                class="min-h-12 rounded-xl border border-zinc-300 data-[pressed]:bg-emerald-800 data-[pressed]:text-white dark:border-zinc-700"
              >
                Homeoffice
              </ToggleGroup.Item>
            </ToggleGroup>
          </div>
        )}
      </form.Field>
      <form.Field name="haendler_name">
        {(field) => (
          <TextField
            label="Händler"
            value={field().state.value}
            onChange={(value) => field().handleChange(value)}
          />
        )}
      </form.Field>
      <form.Field name="haendler_ort">
        {(field) => (
          <TextField
            label="Ort"
            value={field().state.value}
            onChange={(value) => field().handleChange(value)}
          />
        )}
      </form.Field>
      <form.Field name="betrag">
        {(field) => (
          <TextField
            label="Belegbetrag"
            inputmode="decimal"
            value={field().state.value}
            onChange={(value) => field().handleChange(value)}
          />
        )}
      </form.Field>
      <details class="rounded-xl border border-zinc-200 p-3 dark:border-zinc-800">
        <summary>Korrigierter Betrag</summary>
        <div class="mt-3 flex flex-col gap-3">
          <form.Field name="korrigiert">
            {(field) => (
              <TextField
                label="Anerkannter Betrag"
                inputmode="decimal"
                value={field().state.value}
                onChange={(value) => field().handleChange(value)}
              />
            )}
          </form.Field>
          <form.Field name="korrektur_grund">
            {(field) => (
              <TextField
                label="Grund"
                value={field().state.value}
                onChange={(value) => field().handleChange(value)}
              />
            )}
          </form.Field>
        </div>
      </details>
      <form.Field name="notiz">
        {(field) => (
          <TextField
            label="Notiz"
            value={field().state.value}
            onChange={(value) => field().handleChange(value)}
          />
        )}
      </form.Field>
      <Show when={berechnung()}>
        {(calc) => (
          <article class="rounded-2xl bg-zinc-100 p-4 dark:bg-zinc-900">
            <p>Erstattung {formatCent(calc().erstattung_cent)}</p>
            <p>Eigenanteil {formatCent(calc().eigenanteil_cent)}</p>
            <p>Geldwerter Vorteil {formatCent(calc().gv_cent)}</p>
            <p>Steuerfrei {formatCent(calc().steuerfrei_cent)}</p>
            <p>Regulär {formatCent(calc().regulaer_cent)}</p>
          </article>
        )}
      </Show>
      <ul class="flex flex-col gap-2">
        <For each={warnungen()}>
          {(warn) => (
            <li class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
              {warn.text}
            </li>
          )}
        </For>
      </ul>
      <p class="text-sm text-zinc-500">
        Erkennung folgt später. Die Werte werden manuell erfasst.
      </p>
      <Button type="submit">Speichern</Button>
      <Show when={props.beleg}>
        <Button
          class="bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
          onClick={() => {
            if (!props.beleg) {
              return;
            }
            if (monatStatus() === "gesperrt" || monatStatus() === "geaendert") {
              setPending(null);
              setReasonOpen(true);
              return;
            }
            void client
              .deleteBeleg(props.beleg.id, props.beleg.version)
              .then(() => props.onDone());
          }}
        >
          Löschen
        </Button>
      </Show>
      <ReasonDialog
        open={reasonOpen()}
        title="Änderungsgrund"
        onOpenChange={setReasonOpen}
        onConfirm={(reason) => {
          const value = pending();
          setReasonOpen(false);
          if (value) {
            void save(value, reason);
          } else if (props.beleg) {
            void client
              .deleteBeleg(props.beleg.id, props.beleg.version, reason)
              .then(() => props.onDone());
          }
        }}
      />
    </form>
  );
}

export default function Pruefen() {
  const params = useParams();
  const [search] = useSearchParams();
  const navigate = useNavigate();
  const [uploaded, setUploaded] = createSignal<string[]>([]);
  const addFile = async (file: File) => {
    const blob = await downscale(file);
    const bild = await client.upload(blob);
    setUploaded((ids) => [...ids, bild.id].slice(0, 3));
  };
  const existing = useQuery(() => ({
    queryKey: ["beleg", params.id ?? ""],
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
        betrag: (beleg.belegbetrag_cent / 100).toFixed(2).replace(".", ","),
        korrigiert:
          beleg.korrigierter_betrag_cent == null
            ? ""
            : (beleg.korrigierter_betrag_cent / 100)
                .toFixed(2)
                .replace(".", ","),
        korrektur_grund: beleg.korrektur_grund ?? "",
        notiz: beleg.notiz,
      };
    }
    return {
      datum: new Intl.DateTimeFormat("en-CA", {
        timeZone: "Europe/Berlin",
      }).format(new Date()),
      mahlzeit: "mittag",
      bezugsort: "supermarkt",
      arbeitsort: "betrieb",
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

  return (
    <section class="flex flex-col gap-4">
      <h1 class="text-2xl font-semibold">
        {params.id ? "Beleg prüfen" : "Beleg erfassen"}
      </h1>
      <Show when={!params.id && bildIds().length < 3}>
        <CaptureInputs onFile={(file) => void addFile(file)} />
      </Show>
      <Show when={bildIds()[0]}>
        <img
          alt="Belegbild"
          class="max-h-72 w-full rounded-2xl object-contain"
          src={`/api/v1/belegbilder/${bildIds()[0]}/datei`}
        />
      </Show>
      <Show when={!params.id || existing.data}>
        <PruefenForm
          initial={initial()}
          bildIds={bildIds()}
          beleg={existing.data}
          onDone={() => navigate("/monat")}
        />
      </Show>
    </section>
  );
}
