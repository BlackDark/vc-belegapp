import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
// web/public/theme.js is the blocking script in index.html: it paints the
// theme before the app bundle loads, so it lives outside src/ and vitest's
// include pattern never ran it. Reading it here keeps its legacy mapping
// covered. The raw import inlines the file straight from disk, so an edit to
// public/theme.js is what this suite exercises.
import themeScript from "../../public/theme.js?raw";

// Runs the IIFE exactly as the browser would: a fresh global scope against the
// stubbed localStorage and document, so every run starts from a clean paint.
function runThemeScript() {
  new Function(themeScript)();
}

function stubPrefersDark(matches: boolean) {
  const matchMedia = vi.fn(() => ({ matches }));
  vi.stubGlobal("matchMedia", matchMedia);
  return matchMedia;
}

function themeColor(): string | null {
  return (
    document
      .querySelector('meta[name="theme-color"]')
      ?.getAttribute("content") ?? null
  );
}

function isDark(): boolean {
  return document.documentElement.classList.contains("dark");
}

function paintedTheme(): string | undefined {
  return document.documentElement.dataset.theme;
}

beforeEach(() => {
  document.head.innerHTML = '<meta name="theme-color" content="#09090b">';
  document.documentElement.classList.remove("dark");
  delete document.documentElement.dataset.theme;
  localStorage.clear();
});

afterEach(() => {
  localStorage.clear();
  document.documentElement.classList.remove("dark");
  delete document.documentElement.dataset.theme;
  vi.unstubAllGlobals();
});

describe("theme boot script", () => {
  // The regression this guards: a browser profile that stored the pre-rename
  // German value must not paint dark on a light page, which is the flash the
  // migration branch exists to prevent.
  it("paints light for the stored legacy value 'hell'", () => {
    // A dark system preference is the case that would flash if 'hell' fell
    // through to the default instead of being migrated.
    const matchMedia = stubPrefersDark(true);
    localStorage.setItem("belegapp-theme", "hell");

    runThemeScript();

    expect(paintedTheme()).toBe("light");
    expect(isDark()).toBe(false);
    expect(themeColor()).toBe("#ffffff");
    // An explicit choice must never consult the system preference.
    expect(matchMedia).not.toHaveBeenCalled();
  });

  it("paints dark for the stored legacy value 'dunkel'", () => {
    const matchMedia = stubPrefersDark(false);
    localStorage.setItem("belegapp-theme", "dunkel");

    runThemeScript();

    expect(paintedTheme()).toBe("dark");
    expect(isDark()).toBe(true);
    expect(themeColor()).toBe("#09090b");
    expect(matchMedia).not.toHaveBeenCalled();
  });

  it("still paints the current English values", () => {
    stubPrefersDark(true);
    localStorage.setItem("belegapp-theme", "light");
    runThemeScript();
    expect(paintedTheme()).toBe("light");
    expect(isDark()).toBe(false);
    expect(themeColor()).toBe("#ffffff");

    stubPrefersDark(false);
    localStorage.setItem("belegapp-theme", "dark");
    runThemeScript();
    expect(paintedTheme()).toBe("dark");
    expect(isDark()).toBe(true);
    expect(themeColor()).toBe("#09090b");
  });

  // The script's real fallback is the dark app default, not the system
  // preference; only an explicit "system" reads prefers-color-scheme.
  it("falls back to dark when nothing usable is stored", () => {
    const matchMedia = stubPrefersDark(false);
    expect(localStorage.getItem("belegapp-theme")).toBeNull();

    runThemeScript();

    expect(paintedTheme()).toBe("dark");
    expect(isDark()).toBe(true);
    expect(matchMedia).not.toHaveBeenCalled();
  });

  it("falls back to dark for an unrecognised stored value", () => {
    localStorage.setItem("belegapp-theme", "blau");
    runThemeScript();
    expect(paintedTheme()).toBe("dark");
    expect(isDark()).toBe(true);

    localStorage.setItem("belegapp-theme", "");
    runThemeScript();
    expect(paintedTheme()).toBe("dark");
    expect(isDark()).toBe(true);
  });

  it("follows the system preference only for the stored value 'system'", () => {
    localStorage.setItem("belegapp-theme", "system");

    stubPrefersDark(true);
    runThemeScript();
    expect(paintedTheme()).toBe("system");
    expect(isDark()).toBe(true);
    expect(themeColor()).toBe("#09090b");

    stubPrefersDark(false);
    runThemeScript();
    expect(paintedTheme()).toBe("system");
    expect(isDark()).toBe(false);
    expect(themeColor()).toBe("#ffffff");
  });

  it("does not throw when the page has no theme-color meta tag", () => {
    document.head.innerHTML = "";
    localStorage.setItem("belegapp-theme", "hell");

    expect(runThemeScript).not.toThrow();
    expect(paintedTheme()).toBe("light");
    expect(themeColor()).toBeNull();
  });
});
