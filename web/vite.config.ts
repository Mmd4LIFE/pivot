import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Vite builds static assets that the Go binary embeds and serves. Nothing here
// runs in production -- see ADR-0002.
export default defineConfig({
  plugins: [react(), tailwindcss()],

  build: {
    // Emitted into web/dist, which web/embed.go embeds.
    outDir: "dist",
    emptyOutDir: true,

    // Hashed filenames are what make the year-long immutable cache header in
    // the Go handler safe: a changed file has a changed name.
    assetsDir: "assets",

    // The real budget is on *gzipped* size (see the NFRs), which this warning
    // cannot measure -- it reports raw bytes. Part 12 adds the gzipped gate in
    // CI; this is only a local smell detector, so it is set above the vendor
    // chunk rather than tuned to trip on every build.
    chunkSizeWarningLimit: 400,

    sourcemap: true,

    rollupOptions: {
      output: {
        /*
         * Dependencies change far less often than our own code, so splitting
         * them means a deploy that touches only application code leaves those
         * chunk URLs unchanged and a returning browser re-downloads a few
         * kilobytes instead of all of it.
         *
         * A function rather than the object form, because the object form did
         * not do what it said. `{ vendor: ["react", "react-dom"] }` names entry
         * *specifiers*, and the application imports `react-dom/client`, which
         * is a different one -- so React ended up wherever Rollup first needed
         * it, which turned out to be the chunk labelled `tanstack`. The names
         * were lying, and the bundle-size gate is what made that visible.
         */
        manualChunks(id) {
          if (!id.includes("node_modules")) return undefined;

          /*
           * CodeMirror is deliberately unclaimed, and this is the load-bearing
           * line rather than an exception.
           *
           * Naming a chunk here *forces* a module into it, which defeats the
           * dynamic import that was supposed to keep it out of the initial
           * load. Measured: with CodeMirror falling through to "vendor", the
           * vendor chunk went 60.3 KB -> 169.8 KB gzipped and the lazy chunk
           * came out at 0.9 KB holding nothing but our own component. The
           * budget went to 296 KB against a limit of 200.
           *
           * Returning undefined lets Rollup place it where it is actually
           * reached from, which is the editor route's dynamic import.
           *
           * This is the second time the chunking has quietly not done what it
           * says -- see the note above about the object form naming entry
           * specifiers -- and both times the bundle gate is what found it.
           */
          if (/node_modules\/(@codemirror|@lezer|crelt|style-mod|w3c-keyname)\//.test(id)) {
            return undefined;
          }

          if (/node_modules\/(react|react-dom|scheduler)\//.test(id)) return "react";

          /*
           * The virtualizer is excluded for the same reason CodeMirror is: it
           * is only reached from the editor route's dynamic import, and naming
           * a chunk here would force it into the eagerly-loaded one and undo
           * that. TanStack Router and Query stay, because the shell needs them
           * before anything is interactive.
           *
           * `virtual-core` as well as `react-virtual`, and that is not
           * belt-and-braces. The first version matched only the React wrapper,
           * so its core went to the eager chunk and took 7.7 KB of the budget
           * with it while the lazy chunk held 4 KB of our own component. A
           * package excluded here must have its dependencies excluded too, or
           * the exclusion buys nothing.
           */
          if (/node_modules\/@tanstack\/(react-)?virtual(-core)?\//.test(id)) return undefined;

          if (id.includes("node_modules/@tanstack/")) return "tanstack";

          return "vendor";
        },
      },
    },
  },

  server: {
    port: 5173,

    // The dev server proxies the API to the Go process, so the browser sees
    // one origin. That matters for more than convenience: the session cookie
    // is SameSite=Lax and host-only, and a cross-origin dev setup would not
    // send it -- development would behave differently from production in
    // exactly the area hardest to debug.
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: false },
      "/healthz": { target: "http://localhost:8080", changeOrigin: false },
      "/readyz": { target: "http://localhost:8080", changeOrigin: false },
    },
  },
});
