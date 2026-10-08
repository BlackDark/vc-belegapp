(() => {
  var stored = localStorage.getItem("belegapp-theme") || "system";
  var dark =
    stored === "dunkel" ||
    (stored !== "hell" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.dataset.theme = stored;
})();
