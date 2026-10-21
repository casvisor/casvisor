import path from "path";
import {defineConfig} from "vite";
import react from "@vitejs/plugin-react";

const backend = process.env.CASVISOR_BACKEND || "http://localhost:19000";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 18001,
    proxy: {
      // /api also carries the guacd, SSH and VNC websockets
      "/api": {target: backend, changeOrigin: true, ws: true},
      "/swagger": {target: backend, changeOrigin: true},
    },
  },
  build: {
    outDir: "build-temp",
    // "static" as in the CRA build: the backend serves web/build by path, so a
    // build/assets folder would shadow the /assets page
    assetsDir: "static",
    sourcemap: false,
    chunkSizeWarningLimit: 2000,
  },
});
