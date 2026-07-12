export const OFFICE_ARCHIVE_LIMIT = 24 * 1024 * 1024;
export const OFFICE_ENTRY_LIMIT = 4 * 1024 * 1024;
export const OFFICE_TOTAL_LIMIT = 12 * 1024 * 1024;
export const OFFICE_ENTRY_COUNT_LIMIT = 512;
export const OFFICE_XML_ELEMENT_LIMIT = 25_000;
export const SPREADSHEET_CELL_LIMIT = 10_000;
export const SPREADSHEET_SHEET_LIMIT = 100;
export const SPREADSHEET_SHARED_STRING_LIMIT = 10_000;
export const SPREADSHEET_SHARED_TEXT_LIMIT = 1024 * 1024;
export const PRESENTATION_SLIDE_LIMIT = 200;
export const PRESENTATION_PARAGRAPH_LIMIT = 2_000;
export const PRESENTATION_TEXT_LIMIT = 64 * 1024;

export type SpreadsheetCell = { reference: string; value: string };
export type SpreadsheetSheet = { name: string; cells: SpreadsheetCell[]; truncated: boolean };
export type SpreadsheetPreview = { sheets: SpreadsheetSheet[] };
export type PresentationSlide = { title: string; paragraphs: string[] };
export type PresentationPreview = { slides: PresentationSlide[]; truncated: boolean };

type ZipEntry = {
  name: string;
  compressedSize: number;
  uncompressedSize: number;
  compression: number;
  localOffset: number;
};
type ExpansionBudget = { used: number };

const decoder = new TextDecoder("utf-8", { fatal: false });

function u16(bytes: Uint8Array, offset: number): number {
  return bytes[offset] | (bytes[offset + 1] << 8);
}

function u32(bytes: Uint8Array, offset: number): number {
  return (
    (bytes[offset] |
      (bytes[offset + 1] << 8) |
      (bytes[offset + 2] << 16) |
      (bytes[offset + 3] << 24)) >>>
    0
  );
}

function zipEntries(bytes: Uint8Array): Map<string, ZipEntry> {
  const minimum = Math.max(0, bytes.length - 65_557);
  let end = -1;
  for (let offset = bytes.length - 22; offset >= minimum; offset -= 1) {
    if (u32(bytes, offset) === 0x06054b50) {
      end = offset;
      break;
    }
  }
  if (end < 0) throw new Error("This Office file is not a valid ZIP archive.");
  const count = u16(bytes, end + 10);
  if (count > OFFICE_ENTRY_COUNT_LIMIT)
    throw new Error("This Office file has too many archive entries to preview safely.");
  let offset = u32(bytes, end + 16);
  const entries = new Map<string, ZipEntry>();
  for (let index = 0; index < count; index += 1) {
    if (offset + 46 > bytes.length || u32(bytes, offset) !== 0x02014b50)
      throw new Error("This Office archive directory is malformed.");
    const nameLength = u16(bytes, offset + 28);
    const extraLength = u16(bytes, offset + 30);
    const commentLength = u16(bytes, offset + 32);
    const compressedSize = u32(bytes, offset + 20);
    const uncompressedSize = u32(bytes, offset + 24);
    const name = decoder.decode(bytes.subarray(offset + 46, offset + 46 + nameLength));
    if (name.includes("\\") || name.split("/").includes(".."))
      throw new Error("This Office archive contains an unsafe entry path.");
    entries.set(name, {
      name,
      compressedSize,
      uncompressedSize,
      compression: u16(bytes, offset + 10),
      localOffset: u32(bytes, offset + 42),
    });
    offset += 46 + nameLength + extraLength + commentLength;
  }
  return entries;
}

async function entryBytes(
  bytes: Uint8Array,
  entry: ZipEntry,
  signal: AbortSignal,
  budget: ExpansionBudget,
): Promise<Uint8Array> {
  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
  const offset = entry.localOffset;
  if (offset + 30 > bytes.length || u32(bytes, offset) !== 0x04034b50)
    throw new Error("This Office archive contains a malformed entry.");
  const start = offset + 30 + u16(bytes, offset + 26) + u16(bytes, offset + 28);
  const compressed = bytes.subarray(start, start + entry.compressedSize);
  if (compressed.byteLength !== entry.compressedSize)
    throw new Error("This Office archive is truncated.");
  if (
    entry.uncompressedSize > OFFICE_ENTRY_LIMIT ||
    budget.used + entry.uncompressedSize > OFFICE_TOTAL_LIMIT
  ) {
    throw new Error("This Office file expands beyond the safe preview limit.");
  }
  if (entry.compression === 0) {
    if (
      compressed.byteLength !== entry.uncompressedSize ||
      compressed.byteLength > OFFICE_ENTRY_LIMIT
    ) {
      throw new Error("This Office archive entry exceeded its declared safe size.");
    }
    budget.used += compressed.byteLength;
    return compressed.slice();
  }
  if (entry.compression !== 8)
    throw new Error("This Office file uses an unsupported ZIP compression method.");
  const stream = new Blob([new Uint8Array(compressed)])
    .stream()
    .pipeThrough(new DecompressionStream("deflate-raw"));
  const reader = stream.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    while (true) {
      if (signal.aborted) {
        await reader.cancel();
        throw new DOMException("Aborted", "AbortError");
      }
      const { done, value } = await reader.read();
      if (done) break;
      total += value.byteLength;
      if (total > OFFICE_ENTRY_LIMIT || total > entry.uncompressedSize) {
        await reader.cancel();
        throw new Error("This Office archive entry exceeded its declared safe size.");
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  if (total !== entry.uncompressedSize) {
    throw new Error("This Office archive entry exceeded its declared safe size.");
  }
  const output = new Uint8Array(total);
  let outputOffset = 0;
  for (const chunk of chunks) {
    output.set(chunk, outputOffset);
    outputOffset += chunk.byteLength;
  }
  budget.used += output.byteLength;
  return output;
}

async function entryText(
  bytes: Uint8Array,
  entries: Map<string, ZipEntry>,
  name: string,
  signal: AbortSignal,
  budget: ExpansionBudget,
): Promise<string> {
  const entry = entries.get(name);
  if (!entry) throw new Error(`This Office file is missing ${name}.`);
  return decoder.decode(await entryBytes(bytes, entry, signal, budget));
}

function xml(value: string): XMLDocument {
  let elements = 0;
  for (let offset = 0; offset < value.length; offset += 1) {
    if (value.charCodeAt(offset) !== 60) continue;
    const marker = value[offset + 1];
    if (!marker || marker === "/" || marker === "!" || marker === "?" || marker <= " ") continue;
    elements += 1;
    if (elements > OFFICE_XML_ELEMENT_LIMIT) {
      throw new Error("This Office file has too many XML elements to preview safely.");
    }
  }
  const document = new DOMParser().parseFromString(value, "application/xml");
  if (document.querySelector("parsererror"))
    throw new Error("This Office file contains malformed XML.");
  return document;
}

function throwIfAborted(signal: AbortSignal): void {
  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
}

function columnNumber(reference: string): number | null {
  const match = /^([A-Z]+)\d+$/i.exec(reference);
  if (!match) return null;
  let number = 0;
  for (const character of match[1].toUpperCase()) {
    number = number * 26 + character.charCodeAt(0) - 64;
  }
  return number || null;
}

function referenceRow(reference: string): number | null {
  const row = Number(reference.match(/\d+$/)?.[0]);
  return Number.isSafeInteger(row) && row > 0 ? row : null;
}

function columnName(column: number): string {
  let name = "";
  while (column > 0) {
    column -= 1;
    name = String.fromCharCode(65 + (column % 26)) + name;
    column = Math.floor(column / 26);
  }
  return name || "A";
}

function textContent(nodes: HTMLCollectionOf<Element>): string {
  let value = "";
  for (let index = 0; index < nodes.length; index += 1) {
    value += nodes.item(index)?.textContent || "";
  }
  return value;
}

function relationshipMap(document: XMLDocument): Map<string, string> {
  return new Map(
    Array.from(document.getElementsByTagNameNS("*", "Relationship"), (node) => [
      node.getAttribute("Id") || "",
      node.getAttribute("Target") || "",
    ]),
  );
}

function normalizePart(base: string, target: string): string {
  const parts = (target.startsWith("/") ? target : `${base}/${target}`).split("/");
  const clean: string[] = [];
  for (const part of parts) {
    if (!part || part === ".") continue;
    if (part === "..") clean.pop();
    else clean.push(part);
  }
  return clean.join("/");
}

export async function parseSpreadsheet(
  bytes: Uint8Array,
  signal: AbortSignal,
): Promise<SpreadsheetPreview> {
  const entries = zipEntries(bytes);
  const budget = { used: 0 };
  const workbook = xml(await entryText(bytes, entries, "xl/workbook.xml", signal, budget));
  const relationships = relationshipMap(
    xml(await entryText(bytes, entries, "xl/_rels/workbook.xml.rels", signal, budget)),
  );
  const sharedStrings: string[] = [];
  if (entries.has("xl/sharedStrings.xml")) {
    const shared = xml(await entryText(bytes, entries, "xl/sharedStrings.xml", signal, budget));
    const items = shared.getElementsByTagNameNS("*", "si");
    if (items.length > SPREADSHEET_SHARED_STRING_LIMIT) {
      throw new Error("This workbook has too many shared strings to preview safely.");
    }
    let sharedCharacters = 0;
    for (let index = 0; index < items.length; index += 1) {
      throwIfAborted(signal);
      const value = textContent(items.item(index)!.getElementsByTagNameNS("*", "t"));
      sharedCharacters += value.length;
      if (sharedCharacters > SPREADSHEET_SHARED_TEXT_LIMIT) {
        throw new Error("This workbook has too much shared text to preview safely.");
      }
      sharedStrings.push(value);
    }
  }
  const sheets: SpreadsheetSheet[] = [];
  let remainingCells = SPREADSHEET_CELL_LIMIT;
  const sheetDescriptors = workbook.getElementsByTagNameNS("*", "sheet");
  if (sheetDescriptors.length > SPREADSHEET_SHEET_LIMIT) {
    throw new Error("This workbook has too many worksheets to preview safely.");
  }
  for (let sheetIndex = 0; sheetIndex < sheetDescriptors.length; sheetIndex += 1) {
    throwIfAborted(signal);
    const sheet = sheetDescriptors.item(sheetIndex)!;
    const relationID =
      sheet.getAttributeNS(
        "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
        "id",
      ) ||
      sheet.getAttribute("r:id") ||
      "";
    const target = relationships.get(relationID);
    if (!target) continue;
    const worksheet = xml(
      await entryText(bytes, entries, normalizePart("xl", target), signal, budget),
    );
    const cells: SpreadsheetCell[] = [];
    let truncated = false;
    const cellNodes = worksheet.getElementsByTagNameNS("*", "c");
    let previousRow: Element | null = null;
    let fallbackRow = 1;
    let nextColumn = 1;
    for (let cellIndex = 0; cellIndex < cellNodes.length; cellIndex += 1) {
      throwIfAborted(signal);
      if (remainingCells <= 0) {
        truncated = true;
        break;
      }
      const cell = cellNodes.item(cellIndex)!;
      const row = cell.closest("row");
      if (row !== previousRow) {
        const declaredRow = Number(row?.getAttribute("r"));
        fallbackRow =
          Number.isSafeInteger(declaredRow) && declaredRow > 0
            ? declaredRow
            : previousRow
              ? fallbackRow + 1
              : fallbackRow;
        nextColumn = 1;
        previousRow = row;
      }
      const declaredReference = cell.getAttribute("r") || "";
      const declaredColumn = columnNumber(declaredReference);
      fallbackRow = referenceRow(declaredReference) ?? fallbackRow;
      const reference = declaredReference || `${columnName(nextColumn)}${fallbackRow}`;
      nextColumn = declaredColumn ? declaredColumn + 1 : nextColumn + 1;
      const type = cell.getAttribute("t");
      const raw = cell.getElementsByTagNameNS("*", "v").item(0)?.textContent || "";
      const inline = textContent(cell.getElementsByTagNameNS("*", "t"));
      const value =
        type === "s" ? (sharedStrings[Number(raw)] ?? raw) : type === "inlineStr" ? inline : raw;
      cells.push({ reference, value });
      remainingCells -= 1;
    }
    sheets.push({
      name: sheet.getAttribute("name") || `Sheet ${sheets.length + 1}`,
      cells,
      truncated,
    });
  }
  if (!sheets.length) throw new Error("This workbook has no readable worksheets.");
  return { sheets };
}

export async function parsePresentation(
  bytes: Uint8Array,
  signal: AbortSignal,
): Promise<PresentationPreview> {
  const entries = zipEntries(bytes);
  const budget = { used: 0 };
  const presentation = xml(await entryText(bytes, entries, "ppt/presentation.xml", signal, budget));
  const relationships = relationshipMap(
    xml(await entryText(bytes, entries, "ppt/_rels/presentation.xml.rels", signal, budget)),
  );
  const slideIDs = presentation.getElementsByTagNameNS("*", "sldId");
  const truncated = slideIDs.length > PRESENTATION_SLIDE_LIMIT;
  const slides: PresentationSlide[] = [];
  const slideCount = Math.min(slideIDs.length, PRESENTATION_SLIDE_LIMIT);
  for (let slideIndex = 0; slideIndex < slideCount; slideIndex += 1) {
    throwIfAborted(signal);
    const slideID = slideIDs.item(slideIndex)!;
    const relationID =
      slideID.getAttributeNS(
        "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
        "id",
      ) ||
      slideID.getAttribute("r:id") ||
      "";
    const target = relationships.get(relationID);
    if (!target) continue;
    const slide = xml(
      await entryText(bytes, entries, normalizePart("ppt", target), signal, budget),
    );
    const paragraphs: string[] = [];
    let characters = 0;
    const paragraphNodes = slide.getElementsByTagNameNS("*", "p");
    if (paragraphNodes.length > PRESENTATION_PARAGRAPH_LIMIT) {
      throw new Error("A slide contains too many paragraphs to preview safely.");
    }
    for (let paragraphIndex = 0; paragraphIndex < paragraphNodes.length; paragraphIndex += 1) {
      throwIfAborted(signal);
      const paragraph = paragraphNodes.item(paragraphIndex)!;
      const text = textContent(paragraph.getElementsByTagNameNS("*", "t"));
      if (!text) continue;
      characters += text.length;
      if (characters > PRESENTATION_TEXT_LIMIT)
        throw new Error("A slide contains too much text to preview safely.");
      paragraphs.push(text);
    }
    slides.push({ title: paragraphs[0] || `Slide ${slides.length + 1}`, paragraphs });
  }
  if (!slides.length) throw new Error("This presentation has no readable slides.");
  return { slides, truncated };
}
