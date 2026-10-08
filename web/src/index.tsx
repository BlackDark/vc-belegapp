import { registerSW } from "virtual:pwa-register";
import { render } from "solid-js/web";
import { toast } from "solid-sonner";
import App from "./App";
import "./index.css";

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

render(() => <App />, root);
