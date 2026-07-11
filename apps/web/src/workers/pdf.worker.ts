const PDF_IMAGE_LIMIT_WARNING = "Image exceeded maximum allowed size and was removed.";
const originalWarn = console.warn.bind(console);

console.warn = (...values: unknown[]) => {
  if (values.some((value) => String(value).includes(PDF_IMAGE_LIMIT_WARNING))) {
    globalThis.postMessage({ type: "clickclack:pdf-image-limit" });
  }
  originalWarn(...values);
};

void import("pdfjs-dist/build/pdf.worker.mjs");
