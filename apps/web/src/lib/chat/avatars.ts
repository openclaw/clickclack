import { apiResourceURL } from "../api";

const MAX_AVATAR_SOURCE_CACHE = 512;

const avatarSources = new Map<string, string>();

export function avatarImageSource(url?: string | null): string {
  const normalized = apiResourceURL(url?.trim() || "");
  if (!normalized) return "";

  const cached = avatarSources.get(normalized);
  if (cached) return cached;

  if (avatarSources.size >= MAX_AVATAR_SOURCE_CACHE) {
    const oldest = avatarSources.keys().next().value;
    if (oldest) avatarSources.delete(oldest);
  }

  avatarSources.set(normalized, normalized);
  return normalized;
}
