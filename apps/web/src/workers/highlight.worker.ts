import highlighter from "highlight.js";

type HighlightWorkerRequest = { source: string; language: string; outputLimit: number };
type HighlightWorkerResponse = { html: string } | { error: string };

const worker = self as unknown as {
  onmessage: ((event: MessageEvent<HighlightWorkerRequest>) => void) | null;
  postMessage: (message: HighlightWorkerResponse) => void;
};

worker.onmessage = ({ data }) => {
  try {
    if (!highlighter.getLanguage(data.language)) {
      worker.postMessage({ error: "Unsupported syntax language." });
      return;
    }
    const html = highlighter.highlight(data.source, {
      language: data.language,
      ignoreIllegals: true,
    }).value;
    if (html.length > data.outputLimit) {
      worker.postMessage({ error: "Highlighted output exceeded its safety limit." });
      return;
    }
    worker.postMessage({ html });
  } catch {
    worker.postMessage({ error: "Could not highlight this source." });
  }
};
