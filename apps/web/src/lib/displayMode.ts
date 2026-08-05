const DISPLAY_MODE_QUERY = "(display-mode: standalone)";
const DISPLAY_MODE_ATTR = "data-display-mode";

function currentDisplayMode(): "standalone" | "browser" {
  if (typeof window === "undefined") return "browser";
  if ((window.navigator as Navigator & { standalone?: boolean }).standalone === true) return "standalone";
  return window.matchMedia(DISPLAY_MODE_QUERY).matches ? "standalone" : "browser";
}

export function installDisplayModeTracking(): () => void {
  if (typeof window === "undefined") return () => {};

  const media = window.matchMedia(DISPLAY_MODE_QUERY);
  const apply = () => {
    document.documentElement.setAttribute(DISPLAY_MODE_ATTR, currentDisplayMode());
  };

  apply();
  media.addEventListener("change", apply);
  window.addEventListener("appinstalled", apply);

  return () => {
    media.removeEventListener("change", apply);
    window.removeEventListener("appinstalled", apply);
  };
}
