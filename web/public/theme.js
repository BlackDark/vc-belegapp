(() => {
  // Mirrors web/src/lib/theme.ts, including the legacy German values that
  // releases before the English rename may still have in localStorage.
  var stored = localStorage.getItem("belegapp-theme");
  if (stored === "hell") {
    stored = "light";
  } else if (stored === "dunkel") {
    stored = "dark";
  }
  if (stored !== "light" && stored !== "dark" && stored !== "system") {
    stored = "dark";
  }
  var dark =
    stored === "dark" ||
    (stored === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.dataset.theme = stored;
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", dark ? "#09090b" : "#ffffff");
})();
