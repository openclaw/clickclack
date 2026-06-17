// Persisted user preference for opt-in browser notifications. The actual
// browser permission is separate (Notification.permission). Both must
// align before notifications fire.

const STORAGE_PREFIX = "clickclack:browser-notifications-enabled:v1:";

function storageKey(userID: string): string {
  return `${STORAGE_PREFIX}${userID}`;
}

export function readBrowserNotificationsEnabled(userID: string): boolean {
  if (!userID) return false;
  try {
    return window.localStorage.getItem(storageKey(userID)) === "enabled";
  } catch {
    return false;
  }
}

export function writeBrowserNotificationsEnabled(userID: string, enabled: boolean): boolean {
  if (!userID) return false;
  try {
    if (enabled) window.localStorage.setItem(storageKey(userID), "enabled");
    else window.localStorage.removeItem(storageKey(userID));
    return true;
  } catch {
    return false;
  }
}

export function browserNotificationsActive(userID: string): boolean {
  if (!userID) return false;
  if (typeof Notification === "undefined" || Notification.permission !== "granted") return false;
  return readBrowserNotificationsEnabled(userID);
}
