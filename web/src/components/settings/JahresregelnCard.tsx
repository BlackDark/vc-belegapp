import { A } from "@solidjs/router";
import { For } from "solid-js";
import type { Jahresregel } from "../../lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "../ui/card";

export function JahresregelnCard(props: { regeln?: Jahresregel[] }) {
  return (
    <Card class="rounded-xl">
      <CardHeader>
        <CardTitle>Jahresregeln</CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-2">
        <ul class="flex flex-col gap-2">
          <For each={props.regeln ?? []}>
            {(regel) => (
              <li>
                <A
                  class="underline underline-offset-4"
                  href={`/einstellungen/jahre/${regel.jahr}`}
                >
                  {regel.jahr}
                </A>
              </li>
            )}
          </For>
        </ul>
        <A
          class="inline-flex text-sm font-medium underline underline-offset-4"
          href={`/einstellungen/jahre/${new Date().getFullYear()}`}
        >
          Jahr bearbeiten
        </A>
      </CardContent>
    </Card>
  );
}
