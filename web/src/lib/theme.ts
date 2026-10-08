export type Theme = "hell" | "dunkel" | "system";

const key = "belegapp-theme";

export function readTheme(): Theme {
  const stored = localStorage.getItem(key);
  if (stored === "hell" || stored === "dunkel" || stored === "system") {
    return stored;
  }
  return "system";
}

export function applyTheme(theme: Theme) {
  localStorage.setItem(key, theme);
  const dark =
    theme === "dunkel" ||
    (theme === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.dataset.theme = theme;
}
