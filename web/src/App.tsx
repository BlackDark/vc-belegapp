import { appName } from "./meta";

export default function App() {
  return (
    <main class="mx-auto flex min-h-dvh max-w-lg flex-col justify-center gap-4 px-6 py-16">
      <p class="text-sm font-medium tracking-wide text-neutral-500">
        vc-belegapp
      </p>
      <h1 class="text-3xl font-semibold tracking-tight">{appName}</h1>
      <p class="text-lg leading-relaxed text-neutral-700 dark:text-neutral-300">
        Selbst gehostete Erfassung von Essenszuschuss-Belegen.
      </p>
    </main>
  );
}
