import { afterEach, describe, expect, it, vi } from "vitest";
import {
  applyTheme,
  currentTheme,
  isDarkTheme,
  readTheme,
  watchSystemTheme,
} from "./theme";

afterEach(() => {
  localStorage.clear();
  document.documentElement.classList.remove("dark");
  delete document.documentElement.dataset.theme;
  vi.unstubAllGlobals();
});

function installMatchMedia(initial: boolean) {
  let matches = initial;
  const listeners = new Set<() => void>();
  vi.stubGlobal("matchMedia", () => ({
    get matches() {
      return matches;
    },
    media: "(prefers-color-scheme: dark)",
    addEventListener: (_type: string, listener: () => void) => {
      listeners.add(listener);
    },
    removeEventListener: (_type: string, listener: () => void) => {
      listeners.delete(listener);
    },
  }));
  return {
    set(next: boolean) {
      matches = next;
      for (const listener of listeners) listener();
    },
  };
}

describe("theme", () => {
  it("defaults to dunkel", () => {
    expect(readTheme()).toBe("dunkel");
    expect(isDarkTheme("dunkel")).toBe(true);
    expect(isDarkTheme("hell")).toBe(false);
  });

  it("follows the system preference only for system", () => {
    installMatchMedia(true);
    expect(isDarkTheme("system")).toBe(true);
    installMatchMedia(false);
    expect(isDarkTheme("system")).toBe(false);
    expect(isDarkTheme("dunkel")).toBe(true);
  });

  it("stores the choice and toggles the document class", () => {
    document.head.innerHTML = '<meta name="theme-color" content="#09090b">';
    applyTheme("hell");
    expect(currentTheme()).toBe("hell");
    expect(readTheme()).toBe("hell");
    expect(localStorage.getItem("belegapp-theme")).toBe("hell");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(document.documentElement.dataset.theme).toBe("hell");
    expect(
      document
        .querySelector('meta[name="theme-color"]')
        ?.getAttribute("content"),
    ).toBe("#ffffff");

    applyTheme("dunkel");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(
      document
        .querySelector('meta[name="theme-color"]')
        ?.getAttribute("content"),
    ).toBe("#09090b");
  });

  it("reapplies system when the preference changes", () => {
    const media = installMatchMedia(false);
    applyTheme("system");
    const stop = watchSystemTheme();
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    media.set(true);
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    applyTheme("hell");
    media.set(false);
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    stop();
  });
});
