import { parsePresentation, parseSpreadsheet } from "../lib/office-parser";
import type { OfficeKind, PresentationPreview, SpreadsheetPreview } from "../lib/office";

type OfficeWorkerRequest = {
  kind: OfficeKind;
  bytes: Uint8Array<ArrayBuffer>;
};

type OfficeWorkerResponse =
  | { kind: "spreadsheet"; preview: SpreadsheetPreview }
  | { kind: "presentation"; preview: PresentationPreview }
  | { error: string };

const worker = self as unknown as {
  onmessage: ((event: MessageEvent<OfficeWorkerRequest>) => void) | null;
  postMessage: (message: OfficeWorkerResponse) => void;
};

worker.onmessage = ({ data }) => {
  try {
    if (data.kind === "spreadsheet") {
      worker.postMessage({ kind: data.kind, preview: parseSpreadsheet(data.bytes) });
    } else if (data.kind === "presentation") {
      worker.postMessage({ kind: data.kind, preview: parsePresentation(data.bytes) });
    } else {
      worker.postMessage({ error: "Unsupported Office preview type." });
    }
  } catch (error) {
    worker.postMessage({
      error: error instanceof Error ? error.message : "Could not parse this Office file safely.",
    });
  }
};
