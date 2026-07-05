import { contextBridge, ipcRenderer } from "electron";
import type { PublicDesktopSettings } from "./contract";

export type DesktopSettingsInfo = PublicDesktopSettings & {
  platform: NodeJS.Platform;
  supportsAutoLaunch: boolean;
  version: string;
};

export type ServerProbe = {
  detail: string;
  ok: boolean;
  serverUrl?: string;
};

export type ClickClackSettingsBridge = {
  get(): Promise<DesktopSettingsInfo>;
  save(settings: PublicDesktopSettings): Promise<DesktopSettingsInfo>;
  testServer(serverUrl: string): Promise<ServerProbe>;
};

const bridge: ClickClackSettingsBridge = {
  get: () => ipcRenderer.invoke("settings:get"),
  save: (settings) => ipcRenderer.invoke("settings:save", settings),
  testServer: (serverUrl) => ipcRenderer.invoke("settings:test-server", serverUrl),
};

contextBridge.exposeInMainWorld("clickclackSettings", Object.freeze(bridge));
