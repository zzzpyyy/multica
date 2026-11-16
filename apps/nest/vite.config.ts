import { defineConfig } from "vite";

// Development only: forward the same four paths server.mjs forwards in the
// test and production environments, keeping the browser's Host header so the
// server's /ws origin check sees a same-origin request.
const target = process.env.NEST_API_ORIGIN ?? "http://127.0.0.1:8080";
const forward = { target, changeOrigin: false };

export default defineConfig({
  server: {
    proxy: {
      "^/api(/|$)": forward,
      "^/auth(/|$)": forward,
      "^/uploads(/|$)": forward,
      "^/ws(/|$)": { ...forward, ws: true },
    },
  },
  build: { outDir: "dist", emptyOutDir: true },
});
