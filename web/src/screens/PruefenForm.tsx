import { createForm } from "@tanstack/solid-form";
import { useQuery } from "@tanstack/solid-query";
import {
  createEffect,
  createMemo,
  createSignal,
  For,
  onCleanup,
  Show,
} from "solid-js";
import { toast } from "solid-sonner";
import { Choice } from "../components/choice";
import { LabeledField } from "../components/field";
import { ReasonDialog } from "../components/ReasonDialog";
import { Alert, AlertDescription } from "../components/ui/alert";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Card, CardContent } from "../components/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "../components/ui/collapsible";
import { ToggleGroup, ToggleGroupItem } from "../components/ui/toggle-group";
import { bezugsorte, mahlzeitLabel } from "../lib/amtlich";
import {
  ApiError,
  type Beleg,
  client,
  type Erkennung,
  type Warnung,
} from "../lib/api";
import { formatCent, parseEuroToCent } from "../lib/money";
import { queryKeys } from "../lib/queryKeys";

export type Draft = {
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
  const [saving, setSaving] = createSignal(false);
  const jahr = createMemo(
    () => Number(props.initial.datum.slice(0, 4)) || new Date().getFullYear(),
  );
  const regel = useQuery(() => ({
    queryKey: queryKeys.regel(jahr()),
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
    if (saving()) {
      return;
    }
    if (parseEuroToCent(value.betrag) == null) {
      toast.error("Bitte einen gültigen Betrag angeben.");
      return;
    }
    setSaving(true);
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
    } finally {
      setSaving(false);
    }
  };

  const remove = async (grund?: string) => {
    if (!props.beleg || saving()) {
      return;
    }
    setSaving(true);
    try {
      await client.deleteBeleg(props.beleg.id, props.beleg.version, grund);
      props.onDone();
    } catch (err) {
      toast.error(
        err instanceof ApiError ? err.message : "Löschen fehlgeschlagen",
      );
    } finally {
      setSaving(false);
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
    const value = { ...form.state.values };
    const timer = setTimeout(() => void refresh(value), 300);
    onCleanup(() => clearTimeout(timer));
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
          class="animate-pulse rounded-xl border bg-muted px-3 py-3 text-sm"
        >
          Erkennung läuft…
        </p>
      </Show>
      <Show when={manual()}>
        <Alert>
          <AlertDescription>
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
          </AlertDescription>
        </Alert>
      </Show>
      <Show when={showSuggestion()}>
        <div
          data-testid="korrekturvorschlag"
          class="rounded-xl border border-primary px-3 py-3 text-sm"
        >
          <p>
            Vorschlag anerkannter Betrag{" "}
            {formatCent(props.erkennung?.korrekturvorschlag_cent ?? 0)}. Ohne
            Pfand, Alkohol, Tabak und Non-Food.
          </p>
          <div class="mt-3 flex gap-2">
            <Button onClick={acceptSuggestion}>Übernehmen</Button>
            <Button
              variant="outline"
              onClick={() => setIgnored(suggestionKey())}
            >
              Ignorieren
            </Button>
          </div>
        </div>
      </Show>
      <LabeledField
        label="Datum"
        type="date"
        value={values().datum}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, datum: true }));
          form.setFieldValue("datum", value);
        }}
      />
      <fieldset>
        <legend class="mb-1 text-sm font-medium">Mahlzeitart</legend>
        <form.Field name="mahlzeit">
          {(field) => (
            <ToggleGroup
              value={field().state.value}
              onChange={(value) => value && field().handleChange(value)}
              class="grid grid-cols-3 gap-2"
            >
              <For each={meals()}>
                {(meal) => (
                  <ToggleGroupItem value={meal} class="min-h-11">
                    {mahlzeitLabel[meal] ?? meal}
                  </ToggleGroupItem>
                )}
              </For>
            </ToggleGroup>
          )}
        </form.Field>
      </fieldset>
      <Choice
        label="Bezugsort"
        value={values().bezugsort}
        options={bezugsorte}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, bezugsort: true }));
          form.setFieldValue("bezugsort", value);
        }}
      />
      <form.Field name="arbeitsort">
        {(field) => (
          <fieldset>
            <legend class="mb-1 text-sm font-medium">Arbeitsort</legend>
            <ToggleGroup
              value={field().state.value}
              onChange={(value) => value && field().handleChange(value)}
              class="grid grid-cols-2 gap-2"
            >
              <ToggleGroupItem value="betrieb" class="min-h-11">
                Betrieb
              </ToggleGroupItem>
              <ToggleGroupItem value="homeoffice" class="min-h-11">
                Homeoffice
              </ToggleGroupItem>
            </ToggleGroup>
          </fieldset>
        )}
      </form.Field>
      <LabeledField
        label="Händler"
        value={values().haendler_name}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, haendler_name: true }));
          form.setFieldValue("haendler_name", value);
        }}
      />
      <LabeledField
        label="Ort"
        value={values().haendler_ort}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, haendler_ort: true }));
          form.setFieldValue("haendler_ort", value);
        }}
      />
      <LabeledField
        label="Belegbetrag"
        inputmode="decimal"
        value={values().betrag}
        onChange={(value) => {
          setDirty((prev) => ({ ...prev, betrag: true }));
          form.setFieldValue("betrag", value);
        }}
      />
      <Show when={(props.erkennung?.ergebnis?.positionen.length ?? 0) > 0}>
        <Collapsible class="rounded-xl border">
          <CollapsibleTrigger class="flex w-full items-center px-4 py-3 text-left text-sm font-medium">
            Positionen
          </CollapsibleTrigger>
          <CollapsibleContent class="px-4 pb-4">
            <ul class="flex flex-col gap-2">
              <For each={props.erkennung?.ergebnis?.positionen ?? []}>
                {(pos) => (
                  <li class="flex items-center justify-between gap-2 text-sm">
                    <span>{pos.bezeichnung}</span>
                    <Badge variant="secondary">
                      {kategorien[pos.kategorie] ?? pos.kategorie}
                    </Badge>
                    <span class="tabular-nums">
                      {formatCent(pos.betrag_cent)}
                    </span>
                  </li>
                )}
              </For>
            </ul>
          </CollapsibleContent>
        </Collapsible>
      </Show>
      <details
        class="rounded-xl border"
        open={korrekturOffen()}
        onToggle={(event) => setKorrekturOffen(event.currentTarget.open)}
      >
        <summary class="cursor-pointer px-4 py-3 text-sm font-medium">
          Korrigierter Betrag
        </summary>
        <div class="flex flex-col gap-3 px-4 pb-4">
          <LabeledField
            label="Anerkannter Betrag"
            inputmode="decimal"
            value={values().korrigiert}
            onChange={(value) => form.setFieldValue("korrigiert", value)}
          />
          <LabeledField
            label="Grund"
            value={values().korrektur_grund}
            onChange={(value) => form.setFieldValue("korrektur_grund", value)}
          />
        </div>
      </details>
      <LabeledField
        label="Notiz"
        value={values().notiz}
        onChange={(value) => form.setFieldValue("notiz", value)}
      />
      <Show when={berechnung()}>
        {(calc) => (
          <Card class="rounded-xl">
            <CardContent class="grid gap-1 p-4 text-sm">
              <p>Erstattung {formatCent(calc().erstattung_cent)}</p>
              <p>Eigenanteil {formatCent(calc().eigenanteil_cent)}</p>
              <p>Geldwerter Vorteil {formatCent(calc().gv_cent)}</p>
              <p>Steuerfrei {formatCent(calc().steuerfrei_cent)}</p>
              <p>Regulär {formatCent(calc().regulaer_cent)}</p>
            </CardContent>
          </Card>
        )}
      </Show>
      <ul class="flex flex-col gap-2">
        <For each={warnungen()}>
          {(warn) => (
            <li>
              <Alert>
                <AlertDescription>{warn.text}</AlertDescription>
              </Alert>
            </li>
          )}
        </For>
      </ul>
      <Button type="submit" disabled={saving()}>
        Speichern
      </Button>
      <Show when={props.beleg}>
        <Button
          variant="destructive"
          disabled={saving()}
          onClick={() => {
            if (!props.beleg) {
              return;
            }
            if (monatStatus() === "gesperrt" || monatStatus() === "geaendert") {
              setPending(null);
              setReasonOpen(true);
              return;
            }
            void remove();
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
            void remove(reason);
          }
        }}
      />
    </form>
  );
}
