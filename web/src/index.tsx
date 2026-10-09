import { registerSW } from "virtual:pwa-register";
import { render } from "solid-js/web";
import { toast } from "solid-sonner";
import App from "./App";
import { watchSystemTheme } from "./lib/theme";
import "./index.css";

watchSystemTheme();

const updateSW = registerSW({
  onNeedRefresh() {
    toast("Neue Version – neu laden", {
      action: { label: "Neu laden", onClick: () => updateSW(true) },
    });
  },
});

const root = document.getElementById("root");
if (!root) {
  throw new Error("root element missing");
}

// Solid keeps an existing first child and inserts the app after it.
root.replaceChildren();
render(() => <App />, root);
