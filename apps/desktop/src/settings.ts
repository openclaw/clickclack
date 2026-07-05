import type {
  ClickClackSettingsBridge,
  DesktopSettingsInfo,
  ServerProbe,
} from "./settings-preload";

declare global {
  interface Window {
    clickclackSettings: ClickClackSettingsBridge;
  }
}

const form = requiredElement<HTMLFormElement>("settings-form");
const serverInput = requiredElement<HTMLInputElement>("server-url");
const startAtLoginInput = requiredElement<HTMLInputElement>("start-at-login");
const closeToTrayInput = requiredElement<HTMLInputElement>("close-to-tray");
const testButton = requiredElement<HTMLButtonElement>("test-server");
const saveButton = requiredElement<HTMLButtonElement>("save-settings");
const status = requiredElement<HTMLElement>("connection-status");
const platform = requiredElement<HTMLElement>("platform-name");
const version = requiredElement<HTMLElement>("app-version");

let current: DesktopSettingsInfo;

void initialize();

async function initialize() {
  try {
    current = await window.clickclackSettings.get();
    serverInput.value = current.serverUrl;
    startAtLoginInput.checked = current.startAtLogin;
    closeToTrayInput.checked = current.closeToTray;
    startAtLoginInput.disabled = !current.supportsAutoLaunch;
    platform.textContent = platformLabel(current.platform);
    version.textContent = `Desktop ${current.version}`;
    setStatus("Ready to connect", "idle");
  } catch (error) {
    setStatus(errorMessage(error), "error");
  }
}

testButton.addEventListener("click", () => void testConnection());

form.addEventListener("submit", (event) => {
  event.preventDefault();
  void save();
});

async function testConnection() {
  setBusy(true);
  setStatus("Knocking on the server…", "working");
  try {
    const result = await window.clickclackSettings.testServer(serverInput.value);
    showProbe(result);
    if (result.ok && result.serverUrl) serverInput.value = result.serverUrl;
  } catch (error) {
    setStatus(errorMessage(error), "error");
  } finally {
    setBusy(false);
  }
}

async function save() {
  setBusy(true);
  setStatus("Saving and reconnecting…", "working");
  try {
    current = await window.clickclackSettings.save({
      closeToTray: closeToTrayInput.checked,
      serverUrl: serverInput.value,
      startAtLogin: startAtLoginInput.checked,
    });
    setStatus("Connected. Returning to ClickClack…", "success");
  } catch (error) {
    setStatus(errorMessage(error), "error");
    setBusy(false);
  }
}

function showProbe(result: ServerProbe) {
  setStatus(result.detail, result.ok ? "success" : "error");
}

function setBusy(busy: boolean) {
  testButton.disabled = busy;
  saveButton.disabled = busy;
  serverInput.disabled = busy;
  closeToTrayInput.disabled = busy;
  startAtLoginInput.disabled = busy || !current?.supportsAutoLaunch;
}

function setStatus(message: string, state: "error" | "idle" | "success" | "working") {
  status.textContent = message;
  status.dataset.state = state;
}

function platformLabel(value: NodeJS.Platform): string {
  if (value === "darwin") return "macOS";
  if (value === "win32") return "Windows";
  return "Linux";
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function requiredElement<T extends HTMLElement>(id: string): T {
  const element = document.getElementById(id);
  if (!element) throw new Error(`Missing #${id}`);
  return element as T;
}
