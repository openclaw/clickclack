export const OFFICE_ARCHIVE_LIMIT = 24 * 1024 * 1024;
export const OFFICE_ENTRY_LIMIT = 4 * 1024 * 1024;
export const OFFICE_TOTAL_LIMIT = 12 * 1024 * 1024;
export const OFFICE_ENTRY_COUNT_LIMIT = 512;
export const OFFICE_XML_ELEMENT_LIMIT = 25_000;
export const OFFICE_XML_TOTAL_ELEMENT_LIMIT = 100_000;
export const OFFICE_PARSE_TIMEOUT_MS = 5_000;
export const SPREADSHEET_CELL_LIMIT = 10_000;
export const SPREADSHEET_CELL_TEXT_LIMIT = 4 * 1024;
export const SPREADSHEET_TOTAL_TEXT_LIMIT = 1024 * 1024;
export const SPREADSHEET_REFERENCE_LIMIT = 32;
export const SPREADSHEET_SHEET_NAME_LIMIT = 128;
export const SPREADSHEET_ROW_LIMIT = 1_048_576;
export const SPREADSHEET_COLUMN_LIMIT = 16_384;
export const SPREADSHEET_SHEET_LIMIT = 100;
export const SPREADSHEET_SHARED_STRING_LIMIT = 10_000;
export const SPREADSHEET_SHARED_TEXT_LIMIT = 1024 * 1024;
export const PRESENTATION_SLIDE_LIMIT = 200;
export const PRESENTATION_PARAGRAPH_LIMIT = 2_000;
export const PRESENTATION_TEXT_LIMIT = 64 * 1024;
export const PRESENTATION_TOTAL_PARAGRAPH_LIMIT = 10_000;
export const PRESENTATION_TOTAL_TEXT_LIMIT = 1024 * 1024;

export type OfficeKind = "spreadsheet" | "presentation";
export type SpreadsheetCell = { reference: string; value: string };
export type SpreadsheetSheet = { name: string; cells: SpreadsheetCell[]; truncated: boolean };
export type SpreadsheetPreview = { sheets: SpreadsheetSheet[]; hiddenSheets: number };
export type PresentationSlide = { title: string; paragraphs: string[] };
export type PresentationPreview = {
  slides: PresentationSlide[];
  hiddenSlides: number;
  truncated: boolean;
};
export type OfficePreview = SpreadsheetPreview | PresentationPreview;

type OfficeWorkerRequest = {
  kind: OfficeKind;
  bytes: Uint8Array<ArrayBuffer>;
};

type OfficeWorkerResponse =
  | { kind: "spreadsheet"; preview: SpreadsheetPreview }
  | { kind: "presentation"; preview: PresentationPreview }
  | { error: string };

export function parseOfficeInWorker(
  kind: "spreadsheet",
  bytes: Uint8Array,
  signal: AbortSignal,
): Promise<SpreadsheetPreview>;
export function parseOfficeInWorker(
  kind: "presentation",
  bytes: Uint8Array,
  signal: AbortSignal,
): Promise<PresentationPreview>;
export function parseOfficeInWorker(
  kind: OfficeKind,
  bytes: Uint8Array,
  signal: AbortSignal,
): Promise<OfficePreview> {
  return new Promise((resolve, reject) => {
    const worker = new Worker(new URL("../workers/office.worker.ts", import.meta.url), {
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
      finish(() => reject(new DOMException("Office preview was aborted.", "AbortError")));
    const timeout = window.setTimeout(
      () => finish(() => reject(new Error("Office preview took too long and was stopped."))),
      OFFICE_PARSE_TIMEOUT_MS,
    );

    worker.onmessage = (event: MessageEvent<OfficeWorkerResponse>) => {
      const data = event.data;
      if ("error" in data) {
        finish(() => reject(new Error(data.error)));
        return;
      }
      if (data.kind !== kind) {
        finish(() => reject(new Error("Office preview returned an unexpected result.")));
        return;
      }
      finish(() => resolve(data.preview));
    };
    worker.onerror = () =>
      finish(() => reject(new Error("Could not parse this Office file safely.")));
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) {
      abort();
      return;
    }

    const payload =
      bytes.byteOffset === 0 &&
      bytes.buffer instanceof ArrayBuffer &&
      bytes.byteLength === bytes.buffer.byteLength
        ? (bytes as Uint8Array<ArrayBuffer>)
        : bytes.slice();
    const request: OfficeWorkerRequest = { kind, bytes: payload };
    worker.postMessage(request, [payload.buffer]);
  });
}
