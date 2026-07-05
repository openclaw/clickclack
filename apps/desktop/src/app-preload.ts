import { contextBridge, ipcRenderer } from "electron";
import {
  DESKTOP_SERVER_ORIGIN_ARG,
  desktopBridgeAllowed,
  type DesktopNotification,
} from "./contract";

export type ClickClackDesktopBridge = {
  notify(notification: DesktopNotification): Promise<boolean>;
  onNavigate(callback: (route: string) => void): () => void;
  onQuickCompose(callback: () => void): () => void;
  openSettings(): void;
  platform: NodeJS.Platform;
  setActiveRoute(route: string): void;
  setUnreadCount(count: number): void;
  signInWithGitHub(): Promise<boolean>;
};

const bridge: ClickClackDesktopBridge = {
  platform: process.platform,
  notify: (notification) => ipcRenderer.invoke("desktop:notify", notification),
  setUnreadCount: (count) => ipcRenderer.send("desktop:set-unread", count),
  setActiveRoute: (route) => ipcRenderer.send("desktop:set-active-route", route),
  signInWithGitHub: () => ipcRenderer.invoke("desktop:sign-in-with-github"),
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

const trustedOrigin = process.argv
  .find((argument) => argument.startsWith(DESKTOP_SERVER_ORIGIN_ARG))
  ?.slice(DESKTOP_SERVER_ORIGIN_ARG.length);

if (desktopBridgeAllowed(globalThis.location.origin, trustedOrigin)) {
  contextBridge.exposeInMainWorld("clickclackDesktop", Object.freeze(bridge));
}
