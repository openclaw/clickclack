// What this browser can do about web push, and how to name the device. Pure
// helpers with no imports, so they stay testable on their own.

// pushSupported is the capability gate. It runs before any user agent check:
// PushManager is missing in every iOS Safari tab, and iPadOS reports a desktop
// user agent, so sniffing first would answer the wrong question on both.
export function pushSupported(): boolean {
  return (
    typeof navigator !== "undefined" &&
    "serviceWorker" in navigator &&
    typeof window !== "undefined" &&
    "PushManager" in window &&
    "Notification" in window
  );
}

export function installedToHomeScreen(): boolean {
  if (typeof window === "undefined") return false;
  const standalone = (window.navigator as { standalone?: boolean }).standalone;
  if (standalone === true) return true;
  return (
    typeof window.matchMedia === "function" &&
    window.matchMedia("(display-mode: standalone)").matches
  );
}

// appleDevice decides which hint to show, never whether push works. Apple is
// the only platform that requires the home-screen install, and iPadOS only
// admits to being one through its touch points.
export function appleDevice(): boolean {
  if (typeof navigator === "undefined") return false;
  const agent = navigator.userAgent;
  if (/iPhone|iPad|iPod/.test(agent)) return true;
  return agent.includes("Macintosh") && (navigator.maxTouchPoints || 0) > 1;
}

// deviceLabel is the short name shown in settings next to the switch. It is a
// label, not the user agent string, and the server truncates it regardless.
export function deviceLabel(
  userAgent = typeof navigator === "undefined" ? "" : navigator.userAgent,
  touchPoints = typeof navigator === "undefined" ? 0 : navigator.maxTouchPoints || 0,
): string {
  // iPadOS presents itself as a Mac; the touch points give it away.
  const iPadAsMac = /Macintosh/.test(userAgent) && touchPoints > 1;
  const platforms: [RegExp, string][] = [
    [/iPhone/, "iPhone"],
    [/iPad/, "iPad"],
    [/Android/, "Android"],
    [/Macintosh|Mac OS X/, iPadAsMac ? "iPad" : "Mac"],
    [/Windows/, "Windows"],
    [/Linux/, "Linux"],
  ];
  const platform = platforms.find(([pattern]) => pattern.test(userAgent))?.[1] ?? "This device";
  const browser = /Edg\//.test(userAgent)
    ? "Edge"
    : /Chrome\//.test(userAgent)
      ? "Chrome"
      : /Firefox\//.test(userAgent)
        ? "Firefox"
        : /Safari\//.test(userAgent)
          ? "Safari"
          : "";
  return browser ? `${platform} ${browser}` : platform;
}

// sameApplicationServerKey reports whether a subscription's key is the one the
// server signs with now. A browser that does not expose the key cannot be
// compared, so it counts as a match: treating it as stale would replace the
// subscription on every start.
export function sameApplicationServerKey(
  existing: ArrayBuffer | ArrayBufferView | null | undefined,
  configured: Uint8Array,
): boolean {
  if (!existing) return true;
  const bytes = ArrayBuffer.isView(existing)
    ? new Uint8Array(existing.buffer, existing.byteOffset, existing.byteLength)
    : new Uint8Array(existing);
  if (bytes.length !== configured.length) return false;
  return bytes.every((value, index) => value === configured[index]);
}

// applicationServerKey converts the base64url VAPID public key into the byte
// array pushManager.subscribe requires.
export function applicationServerKey(value: string): Uint8Array<ArrayBuffer> {
  const padded = value.trim().replace(/-/g, "+").replace(/_/g, "/");
  const binary = atob(padded + "=".repeat((4 - (padded.length % 4)) % 4));
  // The buffer is allocated explicitly so the array is typed over a plain
  // ArrayBuffer, which is what pushManager.subscribe accepts.
  const bytes = new Uint8Array(new ArrayBuffer(binary.length));
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index);
  return bytes;
}

// subscriptionIsStale decides whether an existing subscription was made under
// a key the server no longer signs with. The server's word wins: it names this
// device stale when it holds the device under a retired key, which it knows
// even when the browser hides the key and the device remembers none.
// Otherwise the browser's own report decides when it gives one, and then the
// key this device remembers subscribing under, because Safari does not expose
// applicationServerKey. With none of the three, the subscription is kept.
export function subscriptionIsStale(
  existing: ArrayBuffer | ArrayBufferView | null | undefined,
  rememberedKey: string,
  configuredKey: string,
  serverSaysStale: boolean,
): boolean {
  if (serverSaysStale) return true;
  const configured = applicationServerKey(configuredKey);
  if (existing) return !sameApplicationServerKey(existing, configured);
  return rememberedKey !== "" && rememberedKey.trim() !== configuredKey.trim();
}

// pushDeviceKey names this browser's subscription to the server without
// sending the endpoint: the unpadded base64url SHA-256 of it, which the server
// computes the same way for each stored device. Without SubtleCrypto it names
// nothing, and the server answers that this is not one of the user's devices.
export async function pushDeviceKey(endpoint: string): Promise<string> {
  const subtle = globalThis.crypto?.subtle;
  if (!endpoint || !subtle) return "";
  const digest = new Uint8Array(await subtle.digest("SHA-256", new TextEncoder().encode(endpoint)));
  let binary = "";
  for (const value of digest) binary += String.fromCharCode(value);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
