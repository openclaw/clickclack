import type { Upload } from "./types";
import { apiURL } from "./api";

export function uploadURL(upload: Upload): string {
  return apiURL(`/api/uploads/${encodeURIComponent(upload.id)}`);
}

export function isImageUpload(upload: Upload): boolean {
  return upload.content_type.startsWith("image/");
}

export function isVideoUpload(upload: Upload): boolean {
  return upload.content_type.startsWith("video/");
}

export function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
