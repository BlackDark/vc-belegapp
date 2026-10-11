import { Route, Router } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { createEffect, createSignal, lazy, onCleanup } from "solid-js";
import Shell from "./components/Shell";
import { Toaster } from "./components/ui/sonner";
import Login from "./screens/Login";

const Today = lazy(() => import("./screens/Today"));
const Capture = lazy(() => import("./screens/Capture"));
const Review = lazy(() => import("./screens/Review"));
const Month = lazy(() => import("./screens/Month"));
const Settings = lazy(() => import("./screens/Settings"));
const AnnualRulePage = lazy(() => import("./screens/AnnualRule"));

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: false, staleTime: 5_000 } },
});

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <Router>
        <Route path="/login" component={Login} />
        <Route path="/" component={Shell}>
          <Route path="/" component={Today} />
          <Route path="/erfassen" component={Capture} />
          <Route path="/belege/neu" component={Review} />
          <Route path="/belege/:id" component={Review} />
          <Route path="/monat" component={Month} />
          <Route path="/einstellungen" component={Settings} />
          <Route path="/einstellungen/jahre/:jahr" component={AnnualRulePage} />
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
