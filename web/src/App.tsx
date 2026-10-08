import { Route, Router } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Toaster } from "solid-sonner";
import Shell from "./components/Shell";
import Einstellungen from "./screens/Einstellungen";
import Erfassen from "./screens/Erfassen";
import Heute from "./screens/Heute";
import JahresregelPage from "./screens/Jahresregel";
import Login from "./screens/Login";
import Monat from "./screens/Monat";
import Pruefen from "./screens/Pruefen";

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
