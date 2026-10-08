import { A, useLocation, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import { CalendarDays, Camera, Settings, Sun } from "lucide-solid";
import { type ParentProps, Show } from "solid-js";
import { ApiError, client } from "../lib/api";

export default function Shell(props: ParentProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const me = useQuery(() => ({
    queryKey: ["me"],
    queryFn: () => client.me(),
    retry: false,
  }));

  return (
    <Show
      when={
        !me.isError ||
        !(me.error instanceof ApiError) ||
        me.error.status !== 401
      }
      fallback={<Redirect to="/login" />}
    >
      <div class="mx-auto min-h-dvh max-w-lg md:max-w-5xl md:pl-56">
        <aside class="fixed inset-y-0 left-0 hidden w-56 flex-col gap-2 border-r border-zinc-200 bg-white p-4 dark:border-zinc-800 dark:bg-zinc-950 md:flex">
          <p class="px-2 text-lg font-semibold">Belegapp</p>
          <NavLink href="/" label="Heute" active={location.pathname === "/"} />
          <NavLink
            href="/monat"
            label="Monat"
            active={location.pathname.startsWith("/monat")}
          />
          <NavLink
            href="/einstellungen"
            label="Einstellungen"
            active={location.pathname.startsWith("/einstellungen")}
          />
        </aside>
        <main class="px-4 pb-28 pt-4">{props.children}</main>
        <nav class="fixed inset-x-0 bottom-0 z-10 border-t border-zinc-200 bg-white/95 px-6 py-2 backdrop-blur dark:border-zinc-800 dark:bg-zinc-950/95 md:hidden">
          <div class="mx-auto grid max-w-lg grid-cols-3 items-end">
            <A href="/" class="flex flex-col items-center gap-1 py-2 text-xs">
              <Sun size={22} /> Heute
            </A>
            <button
              type="button"
              class="mx-auto -mt-7 flex h-14 w-14 items-center justify-center rounded-full bg-emerald-800 text-white shadow dark:bg-emerald-500 dark:text-zinc-950"
              aria-label="Beleg erfassen"
              onClick={() => navigate("/erfassen")}
            >
              <Camera size={24} />
            </button>
            <div class="flex justify-around">
              <A
                href="/monat"
                class="flex flex-col items-center gap-1 py-2 text-xs"
              >
                <CalendarDays size={22} /> Monat
              </A>
              <A
                href="/einstellungen"
                class="flex flex-col items-center gap-1 py-2 text-xs"
              >
                <Settings size={22} /> Mehr
              </A>
            </div>
          </div>
        </nav>
      </div>
    </Show>
  );
}

function NavLink(props: { href: string; label: string; active: boolean }) {
  return (
    <A
      href={props.href}
      class={`rounded-xl px-3 py-2 ${props.active ? "bg-emerald-800 text-white" : ""}`}
    >
      {props.label}
    </A>
  );
}

function Redirect(props: { to: string }) {
  const navigate = useNavigate();
  queueMicrotask(() => navigate(props.to, { replace: true }));
  return <p class="p-6">Weiter zur Anmeldung …</p>;
}
