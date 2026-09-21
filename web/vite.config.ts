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
        // React and the routing/query layer change far less often than our own
        // code. Splitting them means a deploy that touches only application
        // code leaves the vendor chunk's URL unchanged, so a returning browser
        // re-downloads a few kilobytes instead of all of it.
        manualChunks: {
          vendor: ["react", "react-dom"],
          tanstack: ["@tanstack/react-router", "@tanstack/react-query"],
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
