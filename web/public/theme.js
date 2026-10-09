(() => {
  var stored = localStorage.getItem("belegapp-theme");
  if (stored !== "hell" && stored !== "dunkel" && stored !== "system") {
    stored = "dunkel";
  }
  var dark =
    stored === "dunkel" ||
    (stored === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.dataset.theme = stored;
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute("content", dark ? "#09090b" : "#ffffff");
})();
