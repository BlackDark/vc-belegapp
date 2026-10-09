import { ToggleGroup } from "@kobalte/core/toggle-group";
import { useNavigate, useParams, useSearchParams } from "@solidjs/router";
import { createForm } from "@tanstack/solid-form";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import {
  createEffect,
  createMemo,
  createSignal,
  For,
  onCleanup,
  Show,
} from "solid-js";
import { toast } from "solid-sonner";
import { Button, ReasonDialog, TextField } from "../components/ui";
import { bezugsorte, mahlzeitLabel } from "../lib/amtlich";
import {
  ApiError,
  type Beleg,
  client,
  type Erkennung,
  type Warnung,
} from "../lib/api";
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

const korrekturGrund = "Automatisch: ohne Pfand/Alkohol/Tabak/Non-Food";

const kategorien: Record<string, string> = {
  lebensmittel: "Lebensmittel",
  getraenk_alkoholfrei: "Getränk",
  alkohol: "Alkohol",
  tabak: "Tabak",
  pfand: "Pfand",
  nonfood: "Non-Food",
  rabatt: "Rabatt",
  sonstiges: "Sonstiges",
};

function centInput(cents: number): string {
  return (cents / 100).toFixed(2).replace(".", ",");
}

export function PruefenForm(props: {
  initial: Draft;
  bildIds: string[];
  beleg?: Beleg;
  erkennung?: Erkennung;
  timedOut?: boolean;
  onRetry?: () => void;
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
  const [dirty, setDirty] = createSignal<Record<string, boolean>>({});
  const [applied, setApplied] = createSignal("");
  const [ignored, setIgnored] = createSignal("");
  const [korrekturOffen, setKorrekturOffen] = createSignal(false);
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

  createEffect(() => {
    const result = props.erkennung?.ergebnis;
    if (props.beleg || props.erkennung?.status !== "fertig" || !result) {
      return;
    }
    const key = JSON.stringify(result);
    if (applied() === key) {
      return;
    }
    setApplied(key);
    const touched = dirty();
    if (!touched.datum && result.datum) {
      form.setFieldValue("datum", result.datum);
    }
    if (!touched.haendler_name && result.haendler_name) {
      form.setFieldValue("haendler_name", result.haendler_name);
    }
    if (!touched.haendler_ort && result.haendler_ort) {
      form.setFieldValue("haendler_ort", result.haendler_ort);
    }
    if (!touched.betrag && result.gesamtbetrag_cent != null) {
      form.setFieldValue("betrag", centInput(result.gesamtbetrag_cent));
    }
    if (
      !touched.bezugsort &&
      result.bezugsort_vorschlag &&
      bezugsorte.some((item) => item[0] === result.bezugsort_vorschlag)
    ) {
      form.setFieldValue("bezugsort", result.bezugsort_vorschlag);
    }
  });

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

  const values = form.useStore((state) => state.values);
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
  const running = () => {
    const status = props.erkennung?.status;
    return (status === "ausstehend" || status === "laeuft") && !props.timedOut;
  };
  const manual = () =>
    props.timedOut ||
    props.erkennung?.status === "fehler" ||
    props.erkennung?.status === "keine";
  const suggestionKey = () =>
    `${props.erkennung?.korrekturvorschlag_cent ?? ""}:${applied()}`;
  const showSuggestion = () => {
    const cents = props.erkennung?.korrekturvorschlag_cent;
    return cents != null && ignored() !== suggestionKey();
  };
  const acceptSuggestion = () => {
    const cents = props.erkennung?.korrekturvorschlag_cent;
    if (cents == null) {
      return;
    }
    form.setFieldValue("korrigiert", centInput(cents));
    form.setFieldValue("korrektur_grund", korrekturGrund);
    setKorrekturOffen(true);
    setIgnored(suggestionKey());
  };

  return (
    <form
      class="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <Show when={running()}>
        <p
          role="status"
          class="animate-pulse rounded-xl bg-zinc-100 px-3 py-3 text-sm dark:bg-zinc-900"
        >
          Erkennung läuft…
        </p>
      </Show>
      <Show when={manual()}>
        <div class="rounded-xl bg-amber-100 px-3 py-3 text-sm dark:bg-amber-950">
          <p>
            <Show when={props.erkennung?.fehler}>
              {(text) => <span>{text()} </span>}
            </Show>
            Bitte manuell ausfüllen.
          </p>
          <Show when={props.onRetry}>
            <Button class="mt-3" onClick={() => props.onRetry?.()}>
              Erneut erkennen
            </Button>
          </Show>
        </div>
      </Show>
      <Show when={showSuggestion()}>
        <div
          data-testid="korrekturvorschlag"
          class="rounded-xl border border-emerald-700 px-3 py-3 text-sm"
        >
          <p>
            Vorschlag anerkannter Betrag{" "}
            {formatCent(props.erkennung?.korrekturvorschlag_cent ?? 0)}. Ohne
            Pfand, Alkohol, Tabak und Non-Food.
          </p>
          <div class="mt-3 flex gap-2">
            <Button onClick={acceptSuggestion}>Übernehmen</Button>
            <Button
              class="bg-zinc-200 text-zinc-900 dark:bg-zinc-800 dark:text-zinc-100"
              onClick={() => setIgnored(suggestionKey())}
            >
              Ignorieren
            </Button>
          </div>
        </div>
      </Show>
      <TextField
        label="Datum"
        type="date"
        value={values().datum}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, datum: true }));
          form.setFieldValue("datum", value);
        }}
      />
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
      <label class="flex flex-col gap-1 text-sm font-medium">
        Bezugsort
        <select
          class="min-h-12 rounded-xl border border-zinc-300 bg-white px-3 dark:border-zinc-700 dark:bg-zinc-900"
          value={values().bezugsort}
          onChange={(event) => {
            setDirty((prev) => ({ ...prev, bezugsort: true }));
            form.setFieldValue("bezugsort", event.currentTarget.value);
          }}
        >
          <For each={bezugsorte}>
            {(item) => <option value={item[0]}>{item[1]}</option>}
          </For>
        </select>
      </label>
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
      <TextField
        label="Händler"
        value={values().haendler_name}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, haendler_name: true }));
          form.setFieldValue("haendler_name", value);
        }}
      />
      <TextField
        label="Ort"
        value={values().haendler_ort}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, haendler_ort: true }));
          form.setFieldValue("haendler_ort", value);
        }}
      />
      <TextField
        label="Belegbetrag"
        inputmode="decimal"
        value={values().betrag}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, betrag: true }));
          form.setFieldValue("betrag", value);
        }}
      />
      <Show when={(props.erkennung?.ergebnis?.positionen.length ?? 0) > 0}>
        <details class="rounded-xl border border-zinc-200 p-3 dark:border-zinc-800">
          <summary>Positionen</summary>
          <ul class="mt-3 flex flex-col gap-2">
            <For each={props.erkennung?.ergebnis?.positionen ?? []}>
              {(pos) => (
                <li class="flex items-center justify-between gap-2 text-sm">
                  <span>{pos.bezeichnung}</span>
                  <span class="rounded-full bg-zinc-100 px-2 py-1 dark:bg-zinc-800">
                    {kategorien[pos.kategorie] ?? pos.kategorie}
                  </span>
                  <span>{formatCent(pos.betrag_cent)}</span>
                </li>
              )}
            </For>
          </ul>
        </details>
      </Show>
      <details
        class="rounded-xl border border-zinc-200 p-3 dark:border-zinc-800"
        open={korrekturOffen()}
        onToggle={(event) => setKorrekturOffen(event.currentTarget.open)}
      >
        <summary>Korrigierter Betrag</summary>
        <div class="mt-3 flex flex-col gap-3">
          <TextField
            label="Anerkannter Betrag"
            inputmode="decimal"
            value={values().korrigiert}
            onChange={(value) => form.setFieldValue("korrigiert", value)}
          />
          <TextField
            label="Grund"
            value={values().korrektur_grund}
            onChange={(value) => form.setFieldValue("korrektur_grund", value)}
          />
        </div>
      </details>
      <TextField
        label="Notiz"
        value={values().notiz}
        onChange={(value) => form.setFieldValue("notiz", value)}
      />
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
  const queryClient = useQueryClient();
  const [uploaded, setUploaded] = createSignal<string[]>([]);
  const [attempt, setAttempt] = createSignal(0);
  const [gaveUp, setGaveUp] = createSignal(false);
  const addFile = async (file: File) => {
    const blob = await downscale(file);
    const first = bildIds().length === 0;
    const bild = await client.upload(blob, first);
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
  const firstId = () => bildIds()[0] ?? "";
  createEffect(() => {
    firstId();
    attempt();
    setGaveUp(false);
    const timer = setTimeout(() => setGaveUp(true), 90_000);
    onCleanup(() => clearTimeout(timer));
  });
  const bild = useQuery(() => ({
    queryKey: ["bild", firstId()],
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
    await client.erneut(id);
    setAttempt((n) => n + 1);
    await queryClient.invalidateQueries({ queryKey: ["bild", id] });
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
          erkennung={erkennung()}
          timedOut={
            gaveUp() &&
            (erkennung()?.status === "ausstehend" ||
              erkennung()?.status === "laeuft")
          }
          onRetry={firstId() ? () => void retry() : undefined}
          onDone={() => {
            void queryClient.invalidateQueries({ queryKey: ["monat"] });
            navigate("/monat");
          }}
        />
      </Show>
    </section>
  );
}
