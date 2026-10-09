import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { createSignal, Show } from "solid-js";
import { Button, TextField } from "../components/ui";
import { ApiError, client } from "../lib/api";

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
    queryKey: ["auth-config"],
    queryFn: () => client.config(),
  }));
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");

  const submit = async () => {
    setError("");
    try {
      await client.login(password());
      await clientQuery.invalidateQueries({ queryKey: ["me"] });
      navigate("/", { replace: true });
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Anmeldung fehlgeschlagen.",
      );
    }
  };

  return (
    <main class="mx-auto flex min-h-dvh max-w-md flex-col justify-center gap-6 px-6 py-12">
      <div>
        <p class="text-sm text-zinc-500">Essenszuschuss</p>
        <h1 class="text-3xl font-semibold">Belegapp</h1>
      </div>
      <Show when={fehlerText[String(params.fehler ?? "")]}>
        <p class="rounded-xl bg-amber-100 px-3 py-2 text-sm dark:bg-amber-950">
          {fehlerText[String(params.fehler)]}
        </p>
      </Show>
      <Show when={config.data?.oidc}>
        <a
          href="/api/v1/auth/oidc/start"
          rel="external"
          class="flex min-h-14 items-center justify-center rounded-xl bg-emerald-800 text-lg font-medium text-white dark:bg-emerald-500 dark:text-zinc-950"
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
          <TextField
            label="Passwort"
            type="password"
            name="password"
            autocomplete="current-password"
            value={password()}
            onChange={setPassword}
          />
          <Show when={error()}>
            <p class="text-sm text-red-700 dark:text-red-300">{error()}</p>
          </Show>
          <Button type="submit">Anmelden</Button>
        </form>
      </Show>
    </main>
  );
}
