import { Route, Router } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { createEffect, createSignal, lazy, onCleanup } from "solid-js";
import Shell from "./components/Shell";
import { Toaster } from "./components/ui/sonner";
import Login from "./screens/Login";

const Heute = lazy(() => import("./screens/Heute"));
const Erfassen = lazy(() => import("./screens/Erfassen"));
const Pruefen = lazy(() => import("./screens/Pruefen"));
const Monat = lazy(() => import("./screens/Monat"));
const Einstellungen = lazy(() => import("./screens/Einstellungen"));
const JahresregelPage = lazy(() => import("./screens/Jahresregel"));

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: 5_000 } },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <Router>
        <Route path="/login" component={Login} />
        <Route path="/" component={Shell}>
          <Route path="/" component={Heute} />
          <Route path="/erfassen" component={Erfassen} />
          <Route path="/belege/neu" component={Pruefen} />
          <Route path="/belege/:id" component={Pruefen} />
          <Route path="/monat" component={Monat} />
          <Route path="/einstellungen" component={Einstellungen} />
          <Route
            path="/einstellungen/jahre/:jahr"
            component={JahresregelPage}
          />
        </Route>
      </Router>
      <AppToaster />
    </QueryClientProvider>
  );
}

function AppToaster() {
  const [mode, setMode] = createSignal<"light" | "dark">("dark");
  createEffect(() => {
    const sync = () => {
      setMode(
        document.documentElement.classList.contains("dark") ? "dark" : "light",
      );
    };
    sync();
    const observer = new MutationObserver(sync);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    onCleanup(() => observer.disconnect());
  });
  return <Toaster theme={mode()} />;
}
