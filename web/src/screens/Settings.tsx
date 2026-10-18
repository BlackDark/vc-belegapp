import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createEffect, createSignal, Show } from "solid-js";
import { toast } from "solid-sonner";
import { Choice } from "../components/choice";
import { LabeledField } from "../components/field";
import { BelegerkennungCard } from "../components/settings/BelegerkennungCard";
import { DarstellungCard } from "../components/settings/DarstellungCard";
import { DatenexportCard } from "../components/settings/DatenexportCard";
import { DatenimportCard } from "../components/settings/DatenimportCard";
import { JahresregelnCard } from "../components/settings/JahresregelnCard";
import { SitzungenCard } from "../components/settings/SitzungenCard";
import { SpeicherCard } from "../components/settings/SpeicherCard";
import { Alert, AlertDescription } from "../components/ui/alert";
import { Button } from "../components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "../components/ui/card";
import { Checkbox } from "../components/ui/checkbox";
import { Separator } from "../components/ui/separator";
import { Skeleton } from "../components/ui/skeleton";
import {
  client,
  fehlermeldung,
  type Einstellungen as Profile,
} from "../lib/api";
import { queryKeys } from "../lib/queryKeys";
import {
  bezugsorteOptions,
  useEinstellungen,
  useWerte,
} from "../lib/referenceData";

export default function Settings() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const profil = useEinstellungen();
  const regeln = useQuery(() => ({
    queryKey: queryKeys.regeln,
    queryFn: () => client.regeln(),
  }));
  const info = useQuery(() => ({
    queryKey: queryKeys.info,
    queryFn: () => client.info(),
  }));
  const sitzungen = useQuery(() => ({
    queryKey: queryKeys.sitzungen,
    queryFn: () => client.sitzungen(),
  }));
  const werte = useWerte();
  const [name, setName] = createSignal("");
  const [personal, setPersonal] = createSignal("");
  const [ag, setAg] = createSignal("");
  const [bezug, setBezug] = createSignal("supermarkt");
  const [arbeit, setArbeit] = createSignal("betrieb");
  const [aktiv, setAktiv] = createSignal(true);
  const [csvStd, setCsvStd] = createSignal(true);
  const [zipStd, setZipStd] = createSignal(false);
  const [seeded, setSeeded] = createSignal(false);

  createEffect(() => {
    const row = profil.data;
    if (!row || seeded()) {
      return;
    }
    setName(row.arbeitnehmer_name);
    setPersonal(row.personalnummer);
    setAg(row.arbeitgeber_name);
    setBezug(row.standard_bezugsort);
    setArbeit(row.standard_arbeitsort);
    setAktiv(row.erkennung_aktiv);
    setCsvStd(row.export_csv_standard);
    setZipStd(row.export_zip_standard);
    setSeeded(true);
  });

  const save = async (event?: Event) => {
    event?.preventDefault();
    const current = profil.data;
    if (!current) {
      return;
    }
    const next: Profile = {
      ...current,
      arbeitnehmer_name: name(),
      personalnummer: personal(),
      arbeitgeber_name: ag(),
      standard_bezugsort: bezug(),
      standard_arbeitsort: arbeit(),
      erkennung_aktiv: aktiv(),
      export_csv_standard: csvStd(),
      export_zip_standard: zipStd(),
    };
    try {
      await client.putEinstellungen(next);
      toast.success("Gespeichert");
      await queryClient.invalidateQueries({
        queryKey: queryKeys.einstellungen,
      });
    } catch (err) {
      toast.error(fehlermeldung(err, "Speichern fehlgeschlagen"));
    }
  };

  return (
    <section class="flex flex-col gap-6">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Einstellungen</h1>
        <p class="text-sm text-muted-foreground">
          Profil, Jahresregeln, Darstellung und Sicherung.
        </p>
      </div>
      <Show when={profil.isPending}>
        <div class="flex flex-col gap-3">
          <Skeleton height={40} radius={8} />
          <Skeleton height={160} radius={12} />
        </div>
      </Show>
      <Show when={profil.isError}>
        <Alert>
          <AlertDescription>
            Die Einstellungen konnten nicht geladen werden.
          </AlertDescription>
        </Alert>
      </Show>
      <Show when={profil.data}>
        <form
          class="flex flex-col gap-4"
          onSubmit={(event) => void save(event)}
        >
          <Card class="rounded-xl">
            <CardHeader>
              <CardTitle>Profil</CardTitle>
              <CardDescription>
                Name und Standardwerte für neue Belege.
              </CardDescription>
            </CardHeader>
            <CardContent class="flex flex-col gap-3">
              <LabeledField
                label="Arbeitnehmer"
                value={name()}
                onChange={setName}
              />
              <LabeledField
                label="Personalnummer"
                value={personal()}
                onChange={setPersonal}
              />
              <LabeledField label="Arbeitgeber" value={ag()} onChange={setAg} />
              <Show
                when={!werte.isPending}
                fallback={<Skeleton height={40} radius={8} />}
              >
                <Choice
                  label="Standard-Bezugsort"
                  value={bezug()}
                  options={bezugsorteOptions(werte.data)}
                  onChange={setBezug}
                />
              </Show>
              <Choice
                label="Standard-Arbeitsort"
                value={arbeit()}
                options={[
                  ["betrieb", "Betrieb"],
                  ["homeoffice", "Homeoffice"],
                ]}
                onChange={setArbeit}
              />
              <Separator />
              <h2 class="text-lg font-medium">Monatsexport</h2>
              <Checkbox checked={csvStd()} onChange={setCsvStd}>
                CSV im Monatsexport vorauswählen
              </Checkbox>
              <Checkbox checked={zipStd()} onChange={setZipStd}>
                ZIP mit Originalbildern im Monatsexport vorauswählen
              </Checkbox>
              <Button type="submit">Profil speichern</Button>
            </CardContent>
          </Card>
        </form>
      </Show>
      <JahresregelnCard regeln={regeln.data} />
      <DarstellungCard />
      <BelegerkennungCard
        aktiv={aktiv()}
        onAktivChange={(value) => {
          setAktiv(value);
          void save();
        }}
        info={info.data}
      />
      <SpeicherCard backend={info.data?.storage_backend} />
      <DatenexportCard />
      <DatenimportCard />
      <SitzungenCard sitzungen={sitzungen.data} />
      <div class="flex flex-col gap-2">
        <Button
          variant="outline"
          onClick={() =>
            void client
              .protokoll()
              .then((report) =>
                toast(
                  report.ok
                    ? `Kette in Ordnung (${report.anzahl})`
                    : "Kette fehlerhaft",
                ),
              )
          }
        >
          Kette prüfen
        </Button>
        <Button
          variant="outline"
          onClick={() => {
            void (async () => {
              try {
                await client.logout();
                queryClient.clear();
                navigate("/login");
              } catch (err) {
                toast.error(fehlermeldung(err, "Abmelden fehlgeschlagen"));
              }
            })();
          }}
        >
          Abmelden
        </Button>
      </div>
      <p class="text-sm text-muted-foreground">Belegapp {info.data?.version}</p>
    </section>
  );
}
