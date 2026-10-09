import { Route, Router } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { lazy } from "solid-js";
import { Toaster } from "solid-sonner";
import Shell from "./components/Shell";
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
      <Toaster theme="system" />
    </QueryClientProvider>
  );
}
