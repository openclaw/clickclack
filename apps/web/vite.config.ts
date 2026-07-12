import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        // The realtime client connects to /api/realtime/ws on the same origin;
        // without ws upgrades the dev app is stuck on "Reconnecting…".
        ws: true,
      },
    },
  },
});
