import { useQueryClient } from "@tanstack/solid-query";
import { createSignal, Show } from "solid-js";
import { toast } from "solid-sonner";
import { client, fehlermeldung, type SystemInfo } from "../../lib/api";
import { queryKeys } from "../../lib/queryKeys";
import { LabeledField } from "../field";
import { Button } from "../ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../ui/card";
import { Switch, SwitchControl, SwitchLabel, SwitchThumb } from "../ui/switch";

export function BelegerkennungCard(props: {
  aktiv: boolean;
  onAktivChange: (value: boolean) => void;
  info?: SystemInfo;
}) {
  const queryClient = useQueryClient();
  const [testLaeuft, setTestLaeuft] = createSignal(false);

  return (
    <Card class="rounded-xl">
      <CardHeader>
        <CardTitle>Belegerkennung</CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <Switch
          class="flex items-center gap-3"
          checked={props.aktiv}
          onChange={(value) => props.onAktivChange(value)}
        >
          <SwitchControl>
            <SwitchThumb />
          </SwitchControl>
          <SwitchLabel>Belegerkennung aktiv</SwitchLabel>
        </Switch>
        <LabeledField
          label="Modell"
          readOnly
          value={props.info?.llm_model ?? ""}
          onChange={() => undefined}
        />
        <LabeledField
          label="Basis-URL"
          readOnly
          value={props.info?.llm_base_url ?? ""}
          onChange={() => undefined}
        />
        <Show when={props.info?.erkennung_problem}>
          <p class="text-sm text-warning-foreground">
            {props.info?.erkennung_problem}
          </p>
        </Show>
        <Show when={props.info && !props.info.erkennung_konfiguriert}>
          <p class="text-sm text-muted-foreground">
            Nicht konfiguriert. Ohne API-Key bei der OpenAI-Basis-URL oder mit
            BELEGAPP_LLM_ENABLED=false bleibt die Erfassung manuell.
          </p>
        </Show>
        <Button
          variant="outline"
          disabled={testLaeuft()}
          onClick={() => {
            setTestLaeuft(true);
            void client
              .testErkennung()
              .then((result) => {
                if (result.ok) {
                  toast.success(
                    `Verbindung ok (${result.modell}, ${result.dauer_ms} ms)`,
                  );
                } else {
                  toast.error(result.fehler || "Verbindung fehlgeschlagen");
                }
              })
              .catch((err: unknown) => {
                toast.error(fehlermeldung(err, "Verbindung fehlgeschlagen"));
              })
              .finally(() => {
                setTestLaeuft(false);
                void queryClient.invalidateQueries({
                  queryKey: queryKeys.info,
                });
              });
          }}
        >
          Verbindung testen
        </Button>
      </CardContent>
    </Card>
  );
}
