import { createSignal } from "solid-js";

export type Theme = "hell" | "dunkel" | "system";

const key = "belegapp-theme";
const lightColor = "#ffffff";
const darkColor = "#09090b";

const [theme, setTheme] = createSignal<Theme>(readStored());

export function currentTheme(): Theme {
  return theme();
}

function readStored(): Theme {
  if (typeof localStorage === "undefined") {
    return "dunkel";
  }
  const stored = localStorage.getItem(key);
  if (stored === "hell" || stored === "dunkel" || stored === "system") {
    return stored;
  }
  return "dunkel";
}

export function readTheme(): Theme {
  return readStored();
}

export function prefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function isDarkTheme(theme: Theme): boolean {
  return theme === "dunkel" || (theme === "system" && prefersDark());
}

export function applyTheme(next: Theme) {
  setTheme(next);
  localStorage.setItem(key, next);
  const dark = isDarkTheme(next);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.dataset.theme = next;
  const meta = document.querySelector('meta[name="theme-color"]');
  meta?.setAttribute("content", dark ? darkColor : lightColor);
}

export function watchSystemTheme() {
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  const onChange = () => {
    if (readTheme() === "system") {
      applyTheme("system");
    }
  };
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}
