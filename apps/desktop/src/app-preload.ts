import { contextBridge, ipcRenderer } from "electron";
import type { DesktopNotification } from "./contract";

export type ClickClackDesktopBridge = {
  notify(notification: DesktopNotification): Promise<boolean>;
  onNavigate(callback: (route: string) => void): () => void;
  onQuickCompose(callback: () => void): () => void;
  openSettings(): void;
  platform: NodeJS.Platform;
  setActiveRoute(route: string): void;
  setUnreadCount(count: number): void;
};

const bridge: ClickClackDesktopBridge = {
  platform: process.platform,
  notify: (notification) => ipcRenderer.invoke("desktop:notify", notification),
  setUnreadCount: (count) => ipcRenderer.send("desktop:set-unread", count),
  setActiveRoute: (route) => ipcRenderer.send("desktop:set-active-route", route),
  openSettings: () => ipcRenderer.send("desktop:open-settings"),
  onNavigate: (callback) => {
    const listener = (_event: Electron.IpcRendererEvent, route: string) => callback(route);
    ipcRenderer.on("desktop:navigate", listener);
    return () => ipcRenderer.removeListener("desktop:navigate", listener);
  },
  onQuickCompose: (callback) => {
    const listener = () => callback();
    ipcRenderer.on("desktop:quick-compose", listener);
    return () => ipcRenderer.removeListener("desktop:quick-compose", listener);
  },
};

contextBridge.exposeInMainWorld("clickclackDesktop", Object.freeze(bridge));
