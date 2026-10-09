import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createSignal, Show } from "solid-js";

import { LabeledField } from "../components/field";
import { Alert, AlertDescription } from "../components/ui/alert";
import { Button, buttonVariants } from "../components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
} from "../components/ui/card";
import { Skeleton } from "../components/ui/skeleton";
import { ApiError, client } from "../lib/api";
import { queryKeys } from "../lib/queryKeys";
import { cn } from "../lib/utils";

const fehlerText: Record<string, string> = {
  nicht_berechtigt: "Dieses Konto ist nicht berechtigt.",
  oidc: "Die Anmeldung über SSO ist fehlgeschlagen.",
  oidc_nicht_bereit: "SSO ist gerade nicht erreichbar.",
};

export default function Login() {
  const navigate = useNavigate();
  const clientQuery = useQueryClient();
  const [params] = useSearchParams();
  const config = useQuery(() => ({
    queryKey: queryKeys.authConfig,
    queryFn: () => client.config(),
  }));
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");

  const submit = async () => {
    setError("");
    try {
      await client.login(password());
      await clientQuery.invalidateQueries({ queryKey: queryKeys.me });
      navigate("/", { replace: true });
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Anmeldung fehlgeschlagen.",
      );
    }
  };

  return (
    <main class="flex min-h-dvh items-center justify-center bg-background px-4 py-12">
      <Card class="w-full max-w-md rounded-xl">
        <CardHeader>
          <CardDescription>Essenszuschuss</CardDescription>
          <h1 class="text-3xl font-semibold tracking-tight">Belegapp</h1>
        </CardHeader>
        <CardContent class="flex flex-col gap-4">
          <Show when={config.isPending}>
            <Skeleton height={40} radius={8} />
            <Skeleton height={40} radius={8} />
          </Show>
          <Show when={config.isError}>
            <Alert>
              <AlertDescription>
                Die Anmeldung ist gerade nicht erreichbar.
              </AlertDescription>
            </Alert>
          </Show>
          <Show when={fehlerText[String(params.fehler ?? "")]}>
            <Alert>
              <AlertDescription>
                {fehlerText[String(params.fehler)]}
              </AlertDescription>
            </Alert>
          </Show>
          <Show when={config.data?.oidc}>
            <a
              href="/api/v1/auth/oidc/start"
              rel="external"
              class={cn(buttonVariants({ size: "lg" }), "w-full")}
            >
              {config.data?.oidc_label || "Mit SSO anmelden"}
            </a>
          </Show>
          <Show when={config.data?.passwort}>
            <form
              class="flex flex-col gap-3"
              onSubmit={(event) => {
                event.preventDefault();
                void submit();
              }}
            >
              <input
                type="text"
                name="username"
                value="belegapp"
                autocomplete="username"
                class="hidden"
              />
              <LabeledField
                label="Passwort"
                type="password"
                name="password"
                autocomplete="current-password"
                value={password()}
                onChange={setPassword}
              />
              <Show when={error()}>
                <p class="text-sm text-destructive">{error()}</p>
              </Show>
              <Button type="submit" size="lg">
                Anmelden
              </Button>
            </form>
          </Show>
        </CardContent>
      </Card>
    </main>
  );
}
