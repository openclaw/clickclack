import adapter from "@sveltejs/adapter-static";
import { sveltekit } from "@sveltejs/kit/vite";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [
    sveltekit({
      preprocess: vitePreprocess(),
      adapter: adapter({
        pages: "dist",
        assets: "dist",
        fallback: "200.html",
        strict: false,
      }),
      serviceWorker: {
        // The app registers the worker itself only after someone enables push.
        register: false,
      },
      version: {
        name: process.env.CLICKCLACK_WEB_VERSION || "dev",
      },
    }),
  ],
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
