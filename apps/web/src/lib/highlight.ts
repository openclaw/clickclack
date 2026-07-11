export const CODE_HIGHLIGHT_LIMIT = 256 * 1024;
export const CODE_HIGHLIGHT_TIMEOUT_MS = 2_000;
export const CODE_HIGHLIGHT_OUTPUT_LIMIT = 2 * 1024 * 1024;

type HighlightWorkerResponse = { html: string } | { error: string };

function escapeSource(source: string): string {
  const span = document.createElement("span");
  span.textContent = source;
  return span.innerHTML;
}

/**
 * Highlight bounded source in a terminable worker. Larger files deliberately
 * remain escaped plain text so attacker-controlled parsing cannot occupy the UI.
 */
export function highlightCodeInWorker(
  source: string,
  language: string | undefined,
  signal: AbortSignal,
): Promise<string> {
  if (!language || source.length > CODE_HIGHLIGHT_LIMIT) {
    return Promise.resolve(escapeSource(source));
  }

  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL("../workers/highlight.worker.ts", import.meta.url), {
      type: "module",
    });
    let settled = false;

    const finish = (callback: () => void) => {
      if (settled) return;
      settled = true;
      clearTimeout(timeout);
      signal.removeEventListener("abort", abort);
      worker.terminate();
      callback();
    };
    const abort = () =>
      finish(() => reject(new DOMException("Syntax highlighting was aborted.", "AbortError")));
    const timeout = window.setTimeout(
      () => finish(() => resolve(escapeSource(source))),
      CODE_HIGHLIGHT_TIMEOUT_MS,
    );

    worker.onmessage = (event: MessageEvent<HighlightWorkerResponse>) => {
      const data = event.data;
      if ("error" in data) {
        finish(() => resolve(escapeSource(source)));
      } else {
        finish(() => resolve(data.html));
      }
    };
    worker.onerror = () => finish(() => resolve(escapeSource(source)));
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) {
      abort();
      return;
    }
    worker.postMessage({ source, language, outputLimit: CODE_HIGHLIGHT_OUTPUT_LIMIT });
  });
}
