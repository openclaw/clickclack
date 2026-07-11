export const PDF_CANVAS_DIMENSION_LIMIT = 8_192;
export const PDF_CANVAS_PIXEL_LIMIT = 16 * 1024 * 1024;
export const PDF_LOAD_TIMEOUT_MS = 10_000;
export const PDF_RENDER_TIMEOUT_MS = 10_000;

export const PDF_CANVAS_LIMIT_MESSAGE =
  "This PDF page is too large to preview safely. Download the original to open it locally.";

/** Validate the DPR-scaled backing store before assigning canvas dimensions. */
export function assertSafePDFCanvas(width: number, height: number): void {
  if (
    !Number.isSafeInteger(width) ||
    !Number.isSafeInteger(height) ||
    width < 1 ||
    height < 1 ||
    width > PDF_CANVAS_DIMENSION_LIMIT ||
    height > PDF_CANVAS_DIMENSION_LIMIT ||
    width * height > PDF_CANVAS_PIXEL_LIMIT
  ) {
    throw new Error(PDF_CANVAS_LIMIT_MESSAGE);
  }
}
