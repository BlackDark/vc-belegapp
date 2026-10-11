import { createSignal } from "solid-js";

export type Theme = "light" | "dark" | "system";

const key = "belegapp-theme";
const lightColor = "#ffffff";
const darkColor = "#09090b";

// Releases before the English rename persisted the German enum values. They are
// still accepted on read so an existing browser profile keeps its choice; the
// next explicit choice overwrites the entry with an English value.
const legacyThemes: Record<string, Theme> = {
  hell: "light",
  dunkel: "dark",
};

const [theme, setTheme] = createSignal<Theme>(readStored());

export function currentTheme(): Theme {
  return theme();
}

function readStored(): Theme {
  if (typeof localStorage === "undefined") {
    return "dark";
  }
  return normalize(localStorage.getItem(key));
}

function normalize(stored: string | null): Theme {
  if (stored === "light" || stored === "dark" || stored === "system") {
    return stored;
  }
  if (stored !== null && stored in legacyThemes) {
    return legacyThemes[stored];
  }
  return "dark";
}

export function readTheme(): Theme {
  return readStored();
}

export function prefersDark(): boolean {
  return window.matchMedia("(prefers-color-scheme: dark)").matches;
}

export function isDarkTheme(theme: Theme): boolean {
  return theme === "dark" || (theme === "system" && prefersDark());
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
