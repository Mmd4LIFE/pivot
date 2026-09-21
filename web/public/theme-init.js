/*
 * Applies the stored theme before first paint.
 *
 * An external file rather than an inline <script>, and that is a content
 * security policy decision rather than a style one: the shell is served with
 * `script-src 'self'`, which blocks inline scripts outright. Inlined, this ran
 * nowhere and the stored preference was silently ignored -- no console error,
 * no failed request, just the wrong colors.
 *
 * Loaded without defer or async so it is render-blocking. That is the point:
 * running after first paint would show a flash of the wrong theme, which is
 * the entire problem this file exists to prevent.
 */
(function () {
  try {
    var stored = localStorage.getItem("pivot-theme");
    var dark =
      stored === "dark" ||
      (stored !== "light" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.dataset.theme = dark ? "dark" : "light";
  } catch (e) {
    /*
     * Private mode, or storage blocked by policy. The media query in
     * tokens.css still applies the system preference, so the page is themed
     * correctly -- only an explicit override is lost.
     */
  }
})();
