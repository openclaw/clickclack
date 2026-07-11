export const DOCX_EXPANDED_LIMIT = 32 * 1024 * 1024;
export const DOCX_ENTRY_LIMIT = 2_048;
export const DOCX_COMPRESSION_RATIO_LIMIT = 100;
export const DOCX_HTML_LIMIT = 4 * 1024 * 1024;
export const DOCX_CONVERSION_TIMEOUT_MS = 10_000;

const END_OF_CENTRAL_DIRECTORY = 0x06054b50;
const CENTRAL_DIRECTORY_ENTRY = 0x02014b50;
const MAX_END_RECORD_SEARCH = 65_557;

export const DOCX_RESOURCE_LIMIT_MESSAGE =
  "This DOCX is too complex to preview safely. Download the original to open it locally.";

/**
 * Read ZIP metadata without inflating attacker-controlled entries. DOCX packages
 * that use ZIP64 or exceed the preview's expansion boundaries remain downloadable.
 */
export function assertSafeDocxArchive(bytes: ArrayBuffer): void {
  const view = new DataView(bytes);
  let endOffset = -1;
  const searchStart = Math.max(0, view.byteLength - MAX_END_RECORD_SEARCH);

  for (let offset = view.byteLength - 22; offset >= searchStart; offset -= 1) {
    if (view.getUint32(offset, true) === END_OF_CENTRAL_DIRECTORY) {
      endOffset = offset;
      break;
    }
  }
  if (endOffset < 0) throw new Error("This DOCX package is malformed.");

  const entryCount = view.getUint16(endOffset + 10, true);
  const directorySize = view.getUint32(endOffset + 12, true);
  const directoryOffset = view.getUint32(endOffset + 16, true);
  if (
    entryCount === 0xffff ||
    directorySize === 0xffffffff ||
    directoryOffset === 0xffffffff ||
    entryCount > DOCX_ENTRY_LIMIT ||
    directoryOffset + directorySize > endOffset
  ) {
    throw new Error(DOCX_RESOURCE_LIMIT_MESSAGE);
  }

  let offset = directoryOffset;
  let expandedBytes = 0;
  let compressedBytes = 0;
  for (let index = 0; index < entryCount; index += 1) {
    if (offset + 46 > endOffset || view.getUint32(offset, true) !== CENTRAL_DIRECTORY_ENTRY) {
      throw new Error("This DOCX package is malformed.");
    }
    const compressedSize = view.getUint32(offset + 20, true);
    const expandedSize = view.getUint32(offset + 24, true);
    const nameLength = view.getUint16(offset + 28, true);
    const extraLength = view.getUint16(offset + 30, true);
    const commentLength = view.getUint16(offset + 32, true);
    if (compressedSize === 0xffffffff || expandedSize === 0xffffffff) {
      throw new Error(DOCX_RESOURCE_LIMIT_MESSAGE);
    }
    expandedBytes += expandedSize;
    compressedBytes += compressedSize;
    if (expandedBytes > DOCX_EXPANDED_LIMIT) throw new Error(DOCX_RESOURCE_LIMIT_MESSAGE);
    offset += 46 + nameLength + extraLength + commentLength;
  }

  if (
    offset > endOffset ||
    (compressedBytes > 0 && expandedBytes / compressedBytes > DOCX_COMPRESSION_RATIO_LIMIT)
  ) {
    throw new Error(DOCX_RESOURCE_LIMIT_MESSAGE);
  }
}

type DocxWorkerResponse = { html: string } | { error: string };

export function convertDocxInWorker(bytes: ArrayBuffer, signal: AbortSignal): Promise<string> {
  assertSafeDocxArchive(bytes);

  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL("../workers/docx.worker.ts", import.meta.url), {
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
      finish(() => reject(new DOMException("DOCX conversion was aborted.", "AbortError")));
    const timeout = window.setTimeout(
      () => finish(() => reject(new Error("DOCX preview took too long and was stopped."))),
      DOCX_CONVERSION_TIMEOUT_MS,
    );

    worker.onmessage = (event: MessageEvent<DocxWorkerResponse>) => {
      const data = event.data;
      if ("error" in data) {
        finish(() => reject(new Error(data.error)));
      } else {
        finish(() => resolve(data.html));
      }
    };
    worker.onerror = () => finish(() => reject(new Error("Could not convert this DOCX.")));
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) {
      abort();
      return;
    }
    worker.postMessage({ bytes, htmlLimit: DOCX_HTML_LIMIT }, [bytes]);
  });
}
