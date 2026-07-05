/// <reference types="vite/client" />

import type { ClickClackDesktopBridge } from "./lib/desktop";

declare global {
  interface Window {
    clickclackDesktop?: ClickClackDesktopBridge;
  }
}

export {};
