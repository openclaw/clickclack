import adapter from "@sveltejs/adapter-static";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

const config = {
  preprocess: vitePreprocess(),
  kit: {
    adapter: adapter({
      pages: "dist",
      assets: "dist",
      fallback: "200.html",
      strict: false,
    }),
    serviceWorker: {
      // The app registers the worker itself, only after someone turns push
      // on. Nothing should install it on load.
      register: false,
    },
    version: {
      name: process.env.CLICKCLACK_WEB_VERSION || "dev",
    },
  },
};

export default config;
