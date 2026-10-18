import { useQueryClient } from "@tanstack/solid-query";
import { For } from "solid-js";
import { client } from "../../lib/api";
import { queryKeys } from "../../lib/queryKeys";
import { Button } from "../ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../ui/card";

export function SitzungenCard(props: {
  sitzungen?: ReadonlyArray<{ aktuell: boolean; ip: string }>;
}) {
  const queryClient = useQueryClient();

  return (
    <Card class="rounded-xl">
      <CardHeader>
        <CardTitle>Sitzungen</CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <ul class="text-sm">
          <For each={props.sitzungen ?? []}>
            {(row) => (
              <li>
                {row.aktuell ? "Diese Sitzung" : "Weitere Sitzung"} · {row.ip}
              </li>
            )}
          </For>
        </ul>
        <Button
          variant="outline"
          onClick={() =>
            void client.deleteOtherSessions().then(() =>
              queryClient.invalidateQueries({
                queryKey: queryKeys.sitzungen,
              }),
            )
          }
        >
          Andere Sitzungen beenden
        </Button>
      </CardContent>
    </Card>
  );
}
