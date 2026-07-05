import {
  app,
  BrowserWindow,
  dialog,
  ipcMain,
  Menu,
  nativeImage,
  nativeTheme,
  net,
  Notification,
  screen,
  session,
  shell,
  Tray,
  type MenuItemConstructorOptions,
} from "electron";
import { readFile, rename, writeFile } from "node:fs/promises";
import { createHash, randomBytes } from "node:crypto";
import path from "node:path";
import {
  appURL,
  clampUnreadCount,
  deepLinkToRoute,
  defaultSettings,
  DESKTOP_SERVER_ORIGIN_ARG,
  desktopOAuthCallbackCode,
  desktopOAuthStartURL,
  mergeSettings,
  normalizeServerURL,
  safeAppRoute,
  sanitizeNotification,
  type DesktopSettings,
  type PublicDesktopSettings,
  type WindowState,
} from "./contract";

const PROTOCOL = "clickclack";
const SETTINGS_FILE = "desktop.json";
const APP_NAME = "ClickClack";

let mainWindow: BrowserWindow | null = null;
let settingsWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let settings = defaultSettings();
let currentRoute = "/app";
let unreadCount = 0;
let quitting = false;
let routesReady = false;
let pendingRoute: string | null = null;
let pendingProtocolURL: string | null = null;
let pendingDesktopAuth: { serverUrl: string; verifier: string } | null = null;
let windowSaveTimer: NodeJS.Timeout | undefined;
let saveQueue = Promise.resolve();

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  registerProtocol();
  app.on("second-instance", (_event, commandLine) => {
    const link = commandLine.find((argument) => argument.startsWith(`${PROTOCOL}://`));
    if (link) handleProtocolURL(link);
    else openRouteWhenReady(currentRoute);
  });
  app.on("open-url", (event, url) => {
    event.preventDefault();
    handleProtocolURL(url);
  });

  void app.whenReady().then(start);
}

async function start() {
  app.setName(APP_NAME);
  if (process.platform === "win32") app.setAppUserModelId("chat.clickclack.desktop");
  nativeTheme.themeSource = "system";
  settings = await readSettings();
  applyLoginItemSetting();
  registerIPC();
  secureSession();
  installDownloadHandling();
  createApplicationMenu();
  createTray();

  const startupLink = process.argv.find((argument) => argument.startsWith(`${PROTOCOL}://`));
  const initialRoute = pendingRoute ?? (startupLink ? deepLinkToRoute(startupLink) : null);
  pendingRoute = null;
  if (initialRoute) currentRoute = initialRoute;
  createMainWindow(currentRoute);
  routesReady = true;
  if (pendingProtocolURL) {
    const link = pendingProtocolURL;
    pendingProtocolURL = null;
    handleProtocolURL(link);
  } else if (startupLink && desktopOAuthCallbackCode(startupLink)) {
    handleProtocolURL(startupLink);
  }

  app.on("activate", () => showMainWindow());
  app.on("before-quit", () => {
    quitting = true;
  });
  app.on("window-all-closed", () => {
    if (process.platform !== "darwin" && (!settings.closeToTray || quitting)) app.quit();
  });
}

function registerProtocol() {
  if (process.defaultApp && process.argv[1]) {
    app.setAsDefaultProtocolClient(PROTOCOL, process.execPath, [path.resolve(process.argv[1])]);
    return;
  }
  app.setAsDefaultProtocolClient(PROTOCOL);
}

function createMainWindow(route = currentRoute): BrowserWindow {
  if (mainWindow && !mainWindow.isDestroyed()) return mainWindow;
  const saved = visibleWindowState(settings.window);
  const window = new BrowserWindow({
    backgroundColor: "#f7f3ed",
    height: saved.height ?? 860,
    icon: assetPath("icon.png"),
    minHeight: 560,
    minWidth: 760,
    show: false,
    title: APP_NAME,
    width: saved.width ?? 1280,
    ...(saved.x === undefined ? {} : { x: saved.x }),
    ...(saved.y === undefined ? {} : { y: saved.y }),
    webPreferences: {
      additionalArguments: [
        `${DESKTOP_SERVER_ORIGIN_ARG}${normalizeServerURL(settings.serverUrl)}`,
      ],
      backgroundThrottling: false,
      contextIsolation: true,
      nodeIntegration: false,
      preload: distPath("app-preload.cjs"),
      sandbox: true,
      spellcheck: true,
      webSecurity: true,
    },
  });
  mainWindow = window;
  configureWebContents(window);

  window.once("ready-to-show", () => {
    if (saved.maximized) window.maximize();
    window.show();
  });
  window.on("close", (event) => {
    rememberWindowState();
    if (!quitting && settings.closeToTray) {
      event.preventDefault();
      window.hide();
    }
  });
  window.on("closed", () => {
    if (mainWindow === window) mainWindow = null;
  });
  window.on("resize", scheduleWindowStateSave);
  window.on("move", scheduleWindowStateSave);
  window.on("maximize", scheduleWindowStateSave);
  window.on("unmaximize", scheduleWindowStateSave);
  window.webContents.on("did-navigate", (_event, url) => {
    const parsed = routeFromServerURL(url);
    if (parsed) currentRoute = parsed;
  });
  window.webContents.on("render-process-gone", (_event, details) => {
    if (details.reason !== "clean-exit") void window.webContents.reload();
  });
  void window.loadURL(appURL(settings.serverUrl, route));
  return window;
}

function configureWebContents(window: BrowserWindow) {
  window.webContents.setWindowOpenHandler(({ url }) => {
    if (isGitHubLoginStartURL(url)) {
      void beginDesktopOAuth();
    } else if (isSameServerURL(url)) {
      void window.loadURL(url);
    } else if (isExternalURL(url)) {
      void shell.openExternal(url);
    }
    return { action: "deny" };
  });
  window.webContents.on("will-navigate", guardMainFrameNavigation);
  window.webContents.on("will-redirect", guardMainFrameNavigation);
  window.webContents.on("context-menu", (_event, params) => {
    const template: MenuItemConstructorOptions[] = [];
    if (params.misspelledWord) {
      for (const suggestion of params.dictionarySuggestions.slice(0, 5)) {
        template.push({
          label: suggestion,
          click: () => window.webContents.replaceMisspelling(suggestion),
        });
      }
      if (template.length > 0) template.push({ type: "separator" });
      template.push({
        label: `Learn “${params.misspelledWord}”`,
        click: () =>
          window.webContents.session.addWordToSpellCheckerDictionary(params.misspelledWord),
      });
    }
    if (params.isEditable) {
      if (template.length > 0) template.push({ type: "separator" });
      template.push(
        { role: "undo" },
        { role: "redo" },
        { type: "separator" },
        { role: "cut" },
        { role: "copy" },
        { role: "paste" },
        { role: "selectAll" },
      );
    } else if (params.selectionText) {
      template.push({ role: "copy" }, { role: "selectAll" });
    }
    if (params.linkURL && isExternalURL(params.linkURL)) {
      if (template.length > 0) template.push({ type: "separator" });
      template.push({
        label: "Open Link in Browser",
        click: () => void shell.openExternal(params.linkURL),
      });
    }
    if (template.length > 0) Menu.buildFromTemplate(template).popup({ window });
  });
}

function guardMainFrameNavigation(
  event: Electron.Event,
  url: string,
  _isInPlace: boolean,
  isMainFrame: boolean,
) {
  if (!isMainFrame) return;
  if (isGitHubLoginStartURL(url)) {
    event.preventDefault();
    void beginDesktopOAuth();
    return;
  }
  if (isSameServerURL(url)) {
    return;
  }
  event.preventDefault();
  if (url.startsWith(`${PROTOCOL}://`)) {
    openRoute(deepLinkToRoute(url));
  } else if (isExternalURL(url)) {
    void shell.openExternal(url);
  }
}

function createSettingsWindow() {
  if (settingsWindow && !settingsWindow.isDestroyed()) {
    settingsWindow.show();
    settingsWindow.focus();
    return;
  }
  const window = new BrowserWindow({
    backgroundColor: "#17110f",
    height: 720,
    icon: assetPath("icon.png"),
    maximizable: false,
    minHeight: 640,
    minWidth: 620,
    parent: mainWindow ?? undefined,
    resizable: true,
    show: false,
    title: "ClickClack Desktop Settings",
    width: 680,
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      preload: distPath("settings-preload.cjs"),
      sandbox: true,
      webSecurity: true,
    },
  });
  settingsWindow = window;
  window.once("ready-to-show", () => window.show());
  window.on("closed", () => {
    settingsWindow = null;
  });
  window.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
  void window.loadFile(resourcePath("settings.html"));
}

function registerIPC() {
  ipcMain.handle("desktop:notify", (event, input) => {
    if (!isMainSender(event)) return false;
    const payload = sanitizeNotification(input);
    if (!payload || !Notification.isSupported()) return false;
    const notification = new Notification({
      body: payload.body,
      icon: assetPath("icon.png"),
      silent: false,
      title: payload.title,
    });
    notification.on("click", () => openRoute(payload.route ?? currentRoute));
    notification.show();
    return true;
  });
  ipcMain.on("desktop:set-unread", (event, input) => {
    if (!isMainSender(event)) return;
    setUnreadCount(clampUnreadCount(input));
  });
  ipcMain.on("desktop:set-active-route", (event, input) => {
    if (!isMainSender(event) || typeof input !== "string") return;
    const route = safeAppRoute(input);
    if (route) currentRoute = route;
  });
  ipcMain.on("desktop:open-settings", (event) => {
    if (isMainSender(event)) createSettingsWindow();
  });
  ipcMain.handle("desktop:sign-in-with-github", async (event) => {
    if (!isMainSender(event)) return false;
    await beginDesktopOAuth();
    return true;
  });

  ipcMain.handle("settings:get", (event) => {
    requireSettingsSender(event.sender.id);
    return settingsInfo();
  });
  ipcMain.handle("settings:test-server", async (event, input) => {
    requireSettingsSender(event.sender.id);
    const serverUrl = normalizeServerURL(String(input ?? ""));
    try {
      const response = await net.fetch(appURL(serverUrl), {
        method: "HEAD",
        redirect: "follow",
        signal: AbortSignal.timeout(8000),
      });
      if (response.status >= 500) {
        return { detail: `Server answered with HTTP ${response.status}`, ok: false, serverUrl };
      }
      return { detail: `ClickClack answered on ${new URL(serverUrl).host}`, ok: true, serverUrl };
    } catch (error) {
      return {
        detail: error instanceof Error ? error.message : "Could not reach this server",
        ok: false,
        serverUrl,
      };
    }
  });
  ipcMain.handle("settings:save", async (event, input: PublicDesktopSettings) => {
    requireSettingsSender(event.sender.id);
    const next = mergeSettings({
      ...settings,
      ...input,
      serverUrl: normalizeServerURL(input.serverUrl),
    });
    settings = next;
    await persistSettings();
    applyLoginItemSetting();
    currentRoute = "/app";
    if (mainWindow && !mainWindow.isDestroyed()) {
      rememberWindowState();
      const previousWindow = mainWindow;
      mainWindow = null;
      previousWindow.destroy();
      createMainWindow();
    } else {
      createMainWindow();
    }
    setTimeout(() => settingsWindow?.close(), 350);
    return settingsInfo();
  });
}

function settingsInfo() {
  return {
    closeToTray: settings.closeToTray,
    platform: process.platform,
    serverUrl: settings.serverUrl,
    startAtLogin: settings.startAtLogin,
    supportsAutoLaunch: process.platform === "darwin" || process.platform === "win32",
    version: app.getVersion(),
  };
}

function isMainSender(event: Electron.IpcMainEvent | Electron.IpcMainInvokeEvent): boolean {
  return Boolean(
    mainWindow &&
    !mainWindow.isDestroyed() &&
    mainWindow.webContents.id === event.sender.id &&
    event.senderFrame &&
    isSameServerURL(event.senderFrame.url),
  );
}

function requireSettingsSender(id: number) {
  if (!settingsWindow || settingsWindow.isDestroyed() || settingsWindow.webContents.id !== id) {
    throw new Error("Settings request rejected");
  }
}

function secureSession() {
  session.defaultSession.setPermissionRequestHandler((_webContents, _permission, callback) =>
    callback(false),
  );
  session.defaultSession.setPermissionCheckHandler(() => false);
}

function installDownloadHandling() {
  session.defaultSession.on("will-download", (_event, item) => {
    item.once("done", (_doneEvent, state) => {
      if (state !== "completed" || !Notification.isSupported()) return;
      const filePath = item.getSavePath();
      const notification = new Notification({
        body: path.basename(filePath),
        icon: assetPath("icon.png"),
        title: "Download complete",
      });
      notification.on("click", () => shell.showItemInFolder(filePath));
      notification.show();
    });
  });
}

function createApplicationMenu() {
  const template: MenuItemConstructorOptions[] = [];
  if (process.platform === "darwin") {
    template.push({
      label: APP_NAME,
      submenu: [
        { role: "about" },
        { type: "separator" },
        { label: "Settings…", accelerator: "CmdOrCtrl+,", click: createSettingsWindow },
        { type: "separator" },
        { role: "services" },
        { type: "separator" },
        { role: "hide" },
        { role: "hideOthers" },
        { role: "unhide" },
        { type: "separator" },
        { role: "quit" },
      ],
    });
  }
  template.push(
    {
      label: "File",
      submenu: [
        { label: "Quick Compose", accelerator: "CmdOrCtrl+Shift+K", click: quickCompose },
        ...(process.platform === "darwin"
          ? []
          : [
              { type: "separator" as const },
              { label: "Settings…", accelerator: "CmdOrCtrl+,", click: createSettingsWindow },
              { type: "separator" as const },
              { role: "quit" as const },
            ]),
      ],
    },
    {
      label: "Edit",
      submenu: [
        { role: "undo" },
        { role: "redo" },
        { type: "separator" },
        { role: "cut" },
        { role: "copy" },
        { role: "paste" },
        { role: "selectAll" },
      ],
    },
    {
      label: "View",
      submenu: [
        { role: "reload" },
        { role: "forceReload" },
        { type: "separator" },
        { role: "resetZoom" },
        { role: "zoomIn" },
        { role: "zoomOut" },
        { type: "separator" },
        { role: "togglefullscreen" },
      ],
    },
    { label: "Window", submenu: [{ role: "minimize" }, { role: "zoom" }, { role: "close" }] },
  );
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

function createTray() {
  const image = nativeImage.createFromPath(
    assetPath(process.platform === "darwin" ? "trayTemplate.png" : "icon.png"),
  );
  if (process.platform === "darwin") image.setTemplateImage(true);
  tray = new Tray(image.resize({ height: process.platform === "darwin" ? 18 : 20 }));
  tray.setToolTip(APP_NAME);
  tray.on("click", showMainWindow);
  updateTrayMenu();
}

function updateTrayMenu() {
  if (!tray) return;
  const label =
    unreadCount === 0
      ? "No unread messages"
      : `${unreadCount} unread ${unreadCount === 1 ? "message" : "messages"}`;
  tray.setContextMenu(
    Menu.buildFromTemplate([
      { label: "Open ClickClack", click: showMainWindow },
      { label: "Quick Compose", accelerator: "CmdOrCtrl+Shift+K", click: quickCompose },
      { type: "separator" },
      { enabled: false, label },
      { label: "Settings…", click: createSettingsWindow },
      { type: "separator" },
      {
        label: "Quit",
        click: () => {
          quitting = true;
          app.quit();
        },
      },
    ]),
  );
  if (process.platform === "darwin") tray.setTitle(unreadCount > 0 ? String(unreadCount) : "");
}

function setUnreadCount(next: number) {
  if (unreadCount === next) return;
  unreadCount = next;
  app.setBadgeCount(next);
  if (mainWindow && process.platform === "win32") {
    const overlay = next > 0 ? nativeImage.createFromPath(assetPath("unread-badge.png")) : null;
    mainWindow.setOverlayIcon(overlay, next > 0 ? `${next} unread messages` : "");
  }
  updateTrayMenu();
}

function quickCompose() {
  showMainWindow();
  mainWindow?.webContents.send("desktop:quick-compose");
}

function showMainWindow() {
  const window = mainWindow ?? createMainWindow();
  if (window.isMinimized()) window.restore();
  window.show();
  window.focus();
}

function openRoute(route: string | null | undefined) {
  const safeRoute = safeAppRoute(route ?? "") ?? "/app";
  currentRoute = safeRoute;
  const window = mainWindow ?? createMainWindow(safeRoute);
  showMainWindow();
  if (!isSameServerURL(window.webContents.getURL())) {
    void window.loadURL(appURL(settings.serverUrl, safeRoute));
    return;
  }
  if (window.webContents.isLoading()) {
    window.webContents.once("did-finish-load", () =>
      window.webContents.send("desktop:navigate", safeRoute),
    );
  } else {
    window.webContents.send("desktop:navigate", safeRoute);
  }
}

function handleProtocolURL(input: string) {
  const authCode = desktopOAuthCallbackCode(input);
  if (authCode) {
    if (!routesReady) {
      pendingProtocolURL = input;
      return;
    }
    void completeDesktopOAuth(authCode);
    return;
  }
  openRouteWhenReady(deepLinkToRoute(input));
}

async function beginDesktopOAuth() {
  const verifier = randomBytes(32).toString("base64url");
  const challenge = createHash("sha256").update(verifier).digest("base64url");
  const serverUrl = normalizeServerURL(settings.serverUrl);
  pendingDesktopAuth = { serverUrl, verifier };
  showMainWindow();
  try {
    await shell.openExternal(desktopOAuthStartURL(serverUrl, challenge));
  } catch (error) {
    pendingDesktopAuth = null;
    throw error;
  }
}

async function completeDesktopOAuth(code: string) {
  const pending = pendingDesktopAuth;
  if (!pending || pending.serverUrl !== normalizeServerURL(settings.serverUrl)) {
    await showDesktopAuthError("This sign-in request expired. Start again from ClickClack.");
    return;
  }
  try {
    const response = await session.defaultSession.fetch(
      new URL("/api/auth/github/desktop/consume", pending.serverUrl).toString(),
      {
        body: JSON.stringify({ code, code_verifier: pending.verifier }),
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "X-ClickClack-CSRF": "1",
        },
        method: "POST",
        redirect: "error",
        signal: AbortSignal.timeout(10_000),
      },
    );
    if (!response.ok) throw new Error(`Server returned HTTP ${response.status}`);
    const cookies = await session.defaultSession.cookies.get({
      name: "cc_session",
      url: pending.serverUrl,
    });
    if (cookies.length === 0) throw new Error("Server did not create a desktop session");
    pendingDesktopAuth = null;
    currentRoute = "/app";
    const window = mainWindow ?? createMainWindow(currentRoute);
    await window.loadURL(appURL(pending.serverUrl, currentRoute));
    showMainWindow();
  } catch (error) {
    const detail = error instanceof Error ? error.message : "Unknown authentication error";
    await showDesktopAuthError(`GitHub sign-in could not be completed. ${detail}`);
  }
}

async function showDesktopAuthError(message: string) {
  const options: Electron.MessageBoxOptions = {
    message,
    title: "ClickClack sign-in failed",
    type: "error",
  };
  if (mainWindow && !mainWindow.isDestroyed()) {
    await dialog.showMessageBox(mainWindow, options);
  } else {
    await dialog.showMessageBox(options);
  }
}

function openRouteWhenReady(route: string | null | undefined) {
  const safeRoute = safeAppRoute(route ?? "") ?? "/app";
  if (!routesReady) {
    pendingRoute = safeRoute;
    return;
  }
  openRoute(safeRoute);
}

function routeFromServerURL(input: string): string | null {
  try {
    const value = new URL(input);
    if (value.origin !== normalizeServerURL(settings.serverUrl)) return null;
    return safeAppRoute(`${value.pathname}${value.search}${value.hash}`);
  } catch {
    return null;
  }
}

function isSameServerURL(input: string): boolean {
  try {
    return new URL(input).origin === normalizeServerURL(settings.serverUrl);
  } catch {
    return false;
  }
}

function isGitHubLoginStartURL(input: string): boolean {
  try {
    const value = new URL(input);
    return (
      value.origin === normalizeServerURL(settings.serverUrl) &&
      value.pathname === "/api/auth/github/start"
    );
  } catch {
    return false;
  }
}

function isExternalURL(input: string): boolean {
  try {
    return ["https:", "http:", "mailto:"].includes(new URL(input).protocol);
  } catch {
    return false;
  }
}

function visibleWindowState(saved: WindowState | undefined): WindowState {
  if (
    !saved ||
    saved.x === undefined ||
    saved.y === undefined ||
    saved.width === undefined ||
    saved.height === undefined
  ) {
    return saved ?? {};
  }
  const bounds = { x: saved.x, y: saved.y, width: saved.width, height: saved.height };
  const display = screen.getDisplayMatching(bounds);
  const intersects =
    display.workArea.x < bounds.x + bounds.width &&
    display.workArea.x + display.workArea.width > bounds.x &&
    display.workArea.y < bounds.y + bounds.height &&
    display.workArea.y + display.workArea.height > bounds.y;
  return intersects
    ? saved
    : { height: saved.height, maximized: saved.maximized, width: saved.width };
}

function scheduleWindowStateSave() {
  if (windowSaveTimer) clearTimeout(windowSaveTimer);
  windowSaveTimer = setTimeout(rememberWindowState, 300);
}

function rememberWindowState() {
  if (!mainWindow || mainWindow.isDestroyed()) return;
  const bounds = mainWindow.getNormalBounds();
  settings = {
    ...settings,
    window: {
      height: bounds.height,
      maximized: mainWindow.isMaximized(),
      width: bounds.width,
      x: bounds.x,
      y: bounds.y,
    },
  };
  void persistSettings();
}

function applyLoginItemSetting() {
  if (!app.isPackaged || (process.platform !== "darwin" && process.platform !== "win32")) return;
  app.setLoginItemSettings({ openAtLogin: settings.startAtLogin });
}

async function readSettings(): Promise<DesktopSettings> {
  try {
    return mergeSettings(JSON.parse(await readFile(settingsPath(), "utf8")));
  } catch {
    return defaultSettings();
  }
}

function persistSettings(): Promise<void> {
  const snapshot = JSON.stringify(settings, null, 2) + "\n";
  const destination = settingsPath();
  const temporary = `${destination}.tmp`;
  const operation = saveQueue.then(async () => {
    await writeFile(temporary, snapshot, { encoding: "utf8", mode: 0o600 });
    await rename(temporary, destination);
  });
  saveQueue = operation.catch(logSettingsError);
  return operation;
}

function logSettingsError(error: unknown) {
  console.error("Could not save desktop settings", error);
}

function settingsPath(): string {
  return path.join(app.getPath("userData"), SETTINGS_FILE);
}

function distPath(name: string): string {
  return path.join(__dirname, name);
}

function assetPath(name: string): string {
  return path.join(__dirname, "..", "assets", name);
}

function resourcePath(name: string): string {
  return path.join(__dirname, "..", "resources", name);
}

process.on("uncaughtException", (error) => {
  console.error(error);
  if (app.isReady()) void dialog.showErrorBox("ClickClack desktop error", error.message);
});
