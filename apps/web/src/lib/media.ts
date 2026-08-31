export type MediaProbe = {
  width: number;
  height: number;
  durationMS: number;
};

export function probeMediaDimensions(file: File, signal: AbortSignal): Promise<MediaProbe> {
  const empty = { width: 0, height: 0, durationMS: 0 };
  const image = file.type.startsWith("image/");
  if (signal.aborted || (!image && !file.type.startsWith("video/"))) return Promise.resolve(empty);
  return new Promise((resolve) => {
    const url = URL.createObjectURL(file);
    const media = image ? new Image() : document.createElement("video");
    const loadedEvent = image ? "load" : "loadedmetadata";
    function finish(probe = empty) {
      media.removeEventListener(loadedEvent, loaded);
      media.removeEventListener("error", cancelled);
      signal.removeEventListener("abort", cancelled);
      media.removeAttribute("src");
      if (media instanceof HTMLVideoElement) media.load();
      URL.revokeObjectURL(url);
      resolve(probe);
    }
    function cancelled() {
      finish();
    }
    function loaded() {
      finish(
        media instanceof HTMLImageElement
          ? { width: media.naturalWidth, height: media.naturalHeight, durationMS: 0 }
          : {
              width: media.videoWidth,
              height: media.videoHeight,
              durationMS:
                Number.isFinite(media.duration) && media.duration > 0
                  ? Math.round(media.duration * 1000)
                  : 0,
            },
      );
    }
    if (media instanceof HTMLVideoElement) {
      media.preload = "metadata";
      media.muted = true;
    }
    media.addEventListener(loadedEvent, loaded);
    media.addEventListener("error", cancelled);
    signal.addEventListener("abort", cancelled, { once: true });
    media.src = url;
  });
}
