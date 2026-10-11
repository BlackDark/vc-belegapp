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
  it("defaults to dark", () => {
    expect(readTheme()).toBe("dark");
    expect(isDarkTheme("dark")).toBe(true);
    expect(isDarkTheme("light")).toBe(false);
  });

  it("follows the system preference only for system", () => {
    installMatchMedia(true);
    expect(isDarkTheme("system")).toBe(true);
    installMatchMedia(false);
    expect(isDarkTheme("system")).toBe(false);
    expect(isDarkTheme("dark")).toBe(true);
  });

  it("reads the legacy German values written before the rename", () => {
    localStorage.setItem("belegapp-theme", "hell");
    expect(readTheme()).toBe("light");

    localStorage.setItem("belegapp-theme", "dunkel");
    expect(readTheme()).toBe("dark");

    installMatchMedia(true);
    localStorage.setItem("belegapp-theme", "hell");
    expect(isDarkTheme(readTheme())).toBe(false);
    installMatchMedia(false);
    localStorage.setItem("belegapp-theme", "dunkel");
    expect(isDarkTheme(readTheme())).toBe(true);
  });

  it("overwrites the legacy value on the next explicit choice", () => {
    localStorage.setItem("belegapp-theme", "dunkel");
    applyTheme("light");
    expect(localStorage.getItem("belegapp-theme")).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("stores the choice and toggles the document class", () => {
    document.head.innerHTML = '<meta name="theme-color" content="#09090b">';
    applyTheme("light");
    expect(currentTheme()).toBe("light");
    expect(readTheme()).toBe("light");
    expect(localStorage.getItem("belegapp-theme")).toBe("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(
      document
        .querySelector('meta[name="theme-color"]')
        ?.getAttribute("content"),
    ).toBe("#ffffff");

    applyTheme("dark");
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
    applyTheme("light");
    media.set(false);
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    stop();
  });
});
