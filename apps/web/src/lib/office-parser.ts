import { Unzip, UnzipInflate, UnzipPassThrough, type UnzipFile } from "fflate";
import * as sax from "sax";
import type { QualifiedAttribute, QualifiedTag } from "sax";
import {
  OFFICE_ARCHIVE_LIMIT,
  OFFICE_ENTRY_COUNT_LIMIT,
  OFFICE_ENTRY_LIMIT,
  OFFICE_TOTAL_LIMIT,
  OFFICE_XML_ELEMENT_LIMIT,
  OFFICE_XML_TOTAL_ELEMENT_LIMIT,
  PRESENTATION_PARAGRAPH_LIMIT,
  PRESENTATION_SLIDE_LIMIT,
  PRESENTATION_TEXT_LIMIT,
  PRESENTATION_TOTAL_PARAGRAPH_LIMIT,
  PRESENTATION_TOTAL_TEXT_LIMIT,
  SPREADSHEET_CELL_LIMIT,
  SPREADSHEET_CELL_TEXT_LIMIT,
  SPREADSHEET_COLUMN_LIMIT,
  SPREADSHEET_REFERENCE_LIMIT,
  SPREADSHEET_ROW_LIMIT,
  SPREADSHEET_SHARED_STRING_LIMIT,
  SPREADSHEET_SHARED_TEXT_LIMIT,
  SPREADSHEET_SHEET_NAME_LIMIT,
  SPREADSHEET_SHEET_LIMIT,
  SPREADSHEET_TOTAL_TEXT_LIMIT,
  type PresentationPreview,
  type PresentationSlide,
  type SpreadsheetCell,
  type SpreadsheetPreview,
  type SpreadsheetSheet,
} from "./office-types";

type XMLParts = Map<string, Uint8Array<ArrayBuffer>>;
type ParseBudget = { elements: number };
type PresentationOutputBudget = {
  paragraphs: number;
  text: number;
  exhausted: boolean;
  truncated: boolean;
};
type Relationship = { id: string; type: string; target: string };
type XMLHandlers = {
  open?: (tag: QualifiedTag) => void;
  text?: (text: string) => void;
  close?: (tag: QualifiedTag) => void;
};

const RELATIONSHIP_ID_URI = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
const STRICT_RELATIONSHIP_ID_URI = "http://purl.oclc.org/ooxml/officeDocument/relationships";
const ZIP_INPUT_CHUNK_SIZE = 4 * 1024;

class OfficePreviewError extends Error {}

function previewError(message: string): OfficePreviewError {
  return new OfficePreviewError(message);
}

function safeArchiveName(name: string): boolean {
  return (
    name.length > 0 &&
    name.length <= 1024 &&
    !name.startsWith("/") &&
    !name.includes("\\") &&
    !name.includes("\0") &&
    !name.split("/").includes("..")
  );
}

function shouldExtract(name: string): boolean {
  const lower = name.toLowerCase();
  return lower === "[content_types].xml" || lower.endsWith(".xml") || lower.endsWith(".rels");
}

function extractXMLParts(bytes: Uint8Array): XMLParts {
  if (bytes.byteLength > OFFICE_ARCHIVE_LIMIT) {
    throw previewError("This Office file is too large to preview safely.");
  }

  const parts: XMLParts = new Map();
  const seen = new Set<string>();
  let entryCount = 0;
  let expandedBytes = 0;
  let pending = 0;
  let failure: Error | null = null;

  const fail = (error: Error) => {
    failure ??= error;
  };
  const skip = (file: UnzipFile) => {
    file.ondata = () => {};
  };
  const unzip = new Unzip((file) => {
    entryCount += 1;
    if (entryCount > OFFICE_ENTRY_COUNT_LIMIT) {
      fail(previewError("This Office file has too many archive entries to preview safely."));
      skip(file);
      return;
    }
    if (!safeArchiveName(file.name)) {
      fail(previewError("This Office archive contains an unsafe entry path."));
      skip(file);
      return;
    }
    if (seen.has(file.name)) {
      fail(previewError("This Office archive contains duplicate entry names."));
      skip(file);
      return;
    }
    seen.add(file.name);
    if (!shouldExtract(file.name)) {
      skip(file);
      return;
    }
    if (file.compression !== 0 && file.compression !== 8) {
      fail(previewError("This Office file uses an unsupported ZIP compression method."));
      skip(file);
      return;
    }
    if (file.originalSize !== undefined && file.originalSize > OFFICE_ENTRY_LIMIT) {
      fail(previewError("This Office file expands beyond the safe preview limit."));
      skip(file);
      return;
    }

    pending += 1;
    const chunks: Uint8Array<ArrayBuffer>[] = [];
    let entryBytes = 0;
    let completed = false;
    const complete = () => {
      if (completed) return;
      completed = true;
      pending -= 1;
    };
    file.ondata = (error, data, final) => {
      if (error) {
        fail(previewError("This Office archive contains a malformed entry."));
      } else if (!failure) {
        entryBytes += data.byteLength;
        expandedBytes += data.byteLength;
        if (entryBytes > OFFICE_ENTRY_LIMIT || expandedBytes > OFFICE_TOTAL_LIMIT) {
          fail(previewError("This Office file expands beyond the safe preview limit."));
          file.terminate();
        } else {
          chunks.push(data);
        }
      }
      if (!final) return;
      complete();
      if (failure) return;
      if (file.originalSize !== undefined && entryBytes !== file.originalSize) {
        fail(previewError("This Office archive entry exceeded its declared safe size."));
        return;
      }
      const output = new Uint8Array(entryBytes);
      let offset = 0;
      for (const chunk of chunks) {
        output.set(chunk, offset);
        offset += chunk.byteLength;
      }
      parts.set(file.name, output);
    };
    try {
      file.start();
    } catch {
      complete();
      fail(previewError("This Office archive contains a malformed entry."));
    }
  });
  unzip.register(UnzipPassThrough);
  unzip.register(UnzipInflate);

  try {
    for (let offset = 0; offset < bytes.byteLength && !failure; offset += ZIP_INPUT_CHUNK_SIZE) {
      const end = Math.min(offset + ZIP_INPUT_CHUNK_SIZE, bytes.byteLength);
      unzip.push(bytes.subarray(offset, end), end === bytes.byteLength);
    }
  } catch {
    fail(previewError("This Office file is not a valid ZIP archive."));
  }
  if (failure) throw failure;
  if (pending !== 0) throw previewError("This Office archive is truncated.");
  return parts;
}

function decodeXML(bytes: Uint8Array): string {
  let encoding = "utf-8";
  if ((bytes[0] === 0xff && bytes[1] === 0xfe) || (bytes[0] === 0x3c && bytes[1] === 0x00)) {
    encoding = "utf-16le";
  } else if ((bytes[0] === 0xfe && bytes[1] === 0xff) || (bytes[0] === 0x00 && bytes[1] === 0x3c)) {
    encoding = "utf-16be";
  }
  try {
    return new TextDecoder(encoding, { fatal: true }).decode(bytes);
  } catch {
    throw previewError("This Office file contains malformed XML text.");
  }
}

function partText(parts: XMLParts, name: string): string {
  const bytes = parts.get(name);
  if (!bytes) throw previewError(`This Office file is missing ${name}.`);
  return decodeXML(bytes);
}

function parseXML(source: string, part: string, budget: ParseBudget, handlers: XMLHandlers): void {
  let partElements = 0;
  const parser = sax.parser(true, { xmlns: true, position: false });
  const openTags: QualifiedTag[] = [];
  parser.ondoctype = () => {
    throw previewError("This Office file contains an unsupported document type declaration.");
  };
  parser.onopentag = (tag) => {
    const qualifiedTag = tag as QualifiedTag;
    partElements += 1;
    budget.elements += 1;
    if (partElements > OFFICE_XML_ELEMENT_LIMIT) {
      throw previewError("This Office file has too many XML elements to preview safely.");
    }
    if (budget.elements > OFFICE_XML_TOTAL_ELEMENT_LIMIT) {
      throw previewError("This Office file has too much XML structure to preview safely.");
    }
    openTags.push(qualifiedTag);
    handlers.open?.(qualifiedTag);
  };
  parser.ontext = (text) => handlers.text?.(text);
  parser.oncdata = (text) => handlers.text?.(text);
  parser.onclosetag = () => {
    const tag = openTags.pop();
    if (!tag) throw previewError("This Office file contains malformed XML.");
    handlers.close?.(tag);
  };
  parser.onerror = () => {};
  try {
    parser.write(source).close();
    if (parser.error) throw parser.error;
  } catch (error) {
    if (error instanceof OfficePreviewError) throw error;
    throw previewError(`This Office file contains malformed XML in ${part}.`);
  }
}

function attribute(tag: QualifiedTag, local: string, uri?: string): string {
  for (const value of Object.values(tag.attributes) as QualifiedAttribute[]) {
    if (value.local === local && (uri === undefined || value.uri === uri)) return value.value;
  }
  return "";
}

function relationshipID(tag: QualifiedTag): string {
  return (
    attribute(tag, "id", RELATIONSHIP_ID_URI) || attribute(tag, "id", STRICT_RELATIONSHIP_ID_URI)
  );
}

function parseRelationships(source: string, part: string, budget: ParseBudget): Relationship[] {
  const relationships: Relationship[] = [];
  parseXML(source, part, budget, {
    open: (tag) => {
      if (tag.local !== "Relationship") return;
      if (relationships.length >= OFFICE_ENTRY_COUNT_LIMIT) {
        throw previewError("This Office file has too many relationships to preview safely.");
      }
      const id = attribute(tag, "Id");
      const type = attribute(tag, "Type");
      const target = attribute(tag, "Target");
      const targetMode = attribute(tag, "TargetMode");
      if (
        !id ||
        !type ||
        !target ||
        id.length > 256 ||
        type.length > 1024 ||
        target.length > 1024
      ) {
        throw previewError("This Office file contains a malformed relationship.");
      }
      if (targetMode.toLowerCase() === "external") return;
      relationships.push({ id, type, target });
    },
  });
  return relationships;
}

function resolvePart(sourcePart: string, target: string): string {
  if (/^[a-z][a-z0-9+.-]*:/i.test(target) || target.includes("?") || target.includes("#")) {
    throw previewError("This Office file contains an unsafe relationship target.");
  }
  const sourceDirectory = sourcePart.includes("/")
    ? sourcePart.slice(0, sourcePart.lastIndexOf("/"))
    : "";
  const parts = (target.startsWith("/") ? target.slice(1) : `${sourceDirectory}/${target}`).split(
    "/",
  );
  const clean: string[] = [];
  for (const part of parts) {
    if (!part || part === ".") continue;
    if (part === "..") {
      if (!clean.length) throw previewError("This Office file contains an unsafe relationship.");
      clean.pop();
    } else {
      clean.push(part);
    }
  }
  const resolved = clean.join("/");
  if (!safeArchiveName(resolved)) {
    throw previewError("This Office file contains an unsafe relationship target.");
  }
  return resolved;
}

function relationshipPart(part: string): string {
  const slash = part.lastIndexOf("/");
  const directory = slash >= 0 ? part.slice(0, slash + 1) : "";
  const filename = slash >= 0 ? part.slice(slash + 1) : part;
  return `${directory}_rels/${filename}.rels`;
}

function relatedPart(
  sourcePart: string,
  relationships: Relationship[],
  id: string,
  typeSuffix: string,
): string | null {
  const relationship = relationships.find(
    (candidate) => candidate.id === id && candidate.type.endsWith(typeSuffix),
  );
  return relationship ? resolvePart(sourcePart, relationship.target) : null;
}

function officeDocumentPart(parts: XMLParts, budget: ParseBudget): string {
  const rootPart = "_rels/.rels";
  const relationships = parseRelationships(partText(parts, rootPart), rootPart, budget);
  const document = relationships.find((relationship) =>
    relationship.type.endsWith("/officeDocument"),
  );
  if (!document) throw previewError("This Office file has no readable document part.");
  return resolvePart("", document.target);
}

function columnNumber(reference: string): number | null {
  const match = /^([A-Z]+)\d+$/i.exec(reference);
  if (!match) return null;
  let number = 0;
  for (const character of match[1].toUpperCase()) {
    number = number * 26 + character.charCodeAt(0) - 64;
    if (number > SPREADSHEET_COLUMN_LIMIT) return null;
  }
  return number || null;
}

function referenceRow(reference: string): number | null {
  const row = Number(reference.match(/\d+$/)?.[0]);
  return Number.isSafeInteger(row) && row > 0 && row <= SPREADSHEET_ROW_LIMIT ? row : null;
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

function parseSharedStrings(source: string, part: string, budget: ParseBudget): string[] {
  const values: string[] = [];
  let current: string | null = null;
  let textDepth = 0;
  let characters = 0;
  parseXML(source, part, budget, {
    open: (tag) => {
      if (tag.local === "si") {
        if (values.length >= SPREADSHEET_SHARED_STRING_LIMIT) {
          throw previewError("This workbook has too many shared strings to preview safely.");
        }
        current = "";
      } else if (tag.local === "t" && current !== null) {
        textDepth += 1;
      }
    },
    text: (text) => {
      if (current === null || textDepth === 0) return;
      characters += text.length;
      if (characters > SPREADSHEET_SHARED_TEXT_LIMIT) {
        throw previewError("This workbook has too much shared text to preview safely.");
      }
      current += text;
    },
    close: (tag) => {
      if (tag.local === "t" && current !== null) {
        textDepth -= 1;
      } else if (tag.local === "si" && current !== null) {
        values.push(current);
        current = null;
        textDepth = 0;
      }
    },
  });
  return values;
}

type SheetDescriptor = { name: string; relationID: string };

function parseSheetDescriptors(
  source: string,
  part: string,
  budget: ParseBudget,
): { sheets: SheetDescriptor[]; hiddenSheets: number } {
  const sheets: SheetDescriptor[] = [];
  let descriptorCount = 0;
  let hiddenSheets = 0;
  parseXML(source, part, budget, {
    open: (tag) => {
      if (tag.local !== "sheet") return;
      descriptorCount += 1;
      if (descriptorCount > SPREADSHEET_SHEET_LIMIT) {
        throw previewError("This workbook has too many worksheets to preview safely.");
      }
      const state = attribute(tag, "state").toLowerCase();
      if (state && state !== "visible") {
        hiddenSheets += 1;
        return;
      }
      const relationID = relationshipID(tag);
      if (!relationID) throw previewError("This workbook contains a malformed worksheet.");
      const name = attribute(tag, "name") || `Sheet ${sheets.length + 1}`;
      if (name.length > SPREADSHEET_SHEET_NAME_LIMIT) {
        throw previewError("This workbook contains a worksheet name that is too long.");
      }
      sheets.push({
        name,
        relationID,
      });
    },
  });
  return { sheets, hiddenSheets };
}

function parseWorksheet(
  source: string,
  part: string,
  name: string,
  sharedStrings: string[],
  budget: ParseBudget,
  remainingCells: { value: number },
  remainingText: { value: number },
): SpreadsheetSheet {
  const cells: SpreadsheetCell[] = [];
  let truncated = false;
  let fallbackRow = 1;
  let nextColumn = 1;
  let seenRow = false;
  let valueDepth = 0;
  let inlineDepth = 0;
  let current:
    | {
        reference: string;
        type: string;
        raw: string;
        inline: string;
        textTruncated: boolean;
      }
    | undefined;

  parseXML(source, part, budget, {
    open: (tag) => {
      if (tag.local === "row") {
        const declaredRow = Number(attribute(tag, "r"));
        fallbackRow =
          Number.isSafeInteger(declaredRow) && declaredRow > 0
            ? declaredRow
            : seenRow
              ? fallbackRow + 1
              : fallbackRow;
        nextColumn = 1;
        seenRow = true;
        return;
      }
      if (tag.local === "c") {
        if (remainingCells.value <= 0) {
          truncated = true;
          current = undefined;
          return;
        }
        const declaredReference = attribute(tag, "r");
        if (declaredReference.length > SPREADSHEET_REFERENCE_LIMIT) {
          throw previewError("This workbook contains an invalid cell reference.");
        }
        const declaredColumn = columnNumber(declaredReference);
        const declaredRow = referenceRow(declaredReference);
        if (declaredReference && (!declaredColumn || !declaredRow)) {
          throw previewError("This workbook contains an invalid cell reference.");
        }
        fallbackRow = declaredRow ?? fallbackRow;
        current = {
          reference: declaredReference || `${columnName(nextColumn)}${fallbackRow}`,
          type: attribute(tag, "t"),
          raw: "",
          inline: "",
          textTruncated: false,
        };
        nextColumn = declaredColumn ? declaredColumn + 1 : nextColumn + 1;
      } else if (current && tag.local === "v") {
        valueDepth += 1;
      } else if (current && tag.local === "t") {
        inlineDepth += 1;
      }
    },
    text: (text) => {
      if (!current) return;
      const key = valueDepth > 0 ? "raw" : inlineDepth > 0 ? "inline" : null;
      if (!key) return;
      const remaining = SPREADSHEET_CELL_TEXT_LIMIT - current[key].length;
      if (remaining <= 0) {
        current.textTruncated = true;
        truncated = true;
        return;
      }
      current[key] += text.slice(0, remaining);
      if (text.length > remaining) {
        current.textTruncated = true;
        truncated = true;
      }
    },
    close: (tag) => {
      if (tag.local === "v" && current) {
        valueDepth -= 1;
        return;
      }
      if (tag.local === "t" && current) {
        inlineDepth -= 1;
        return;
      }
      if (tag.local !== "c" || !current) return;
      const value =
        current.type === "s"
          ? (sharedStrings[Number(current.raw)] ?? current.raw)
          : current.type === "inlineStr"
            ? current.inline
            : current.type === "b"
              ? current.raw === "1"
                ? "TRUE"
                : current.raw === "0"
                  ? "FALSE"
                  : current.raw
              : current.raw;
      const available = Math.min(SPREADSHEET_CELL_TEXT_LIMIT, Math.max(0, remainingText.value));
      const boundedValue = value.slice(0, available);
      remainingText.value -= boundedValue.length;
      if (boundedValue.length < value.length || current.textTruncated) truncated = true;
      cells.push({ reference: current.reference, value: boundedValue });
      remainingCells.value -= 1;
      current = undefined;
      valueDepth = 0;
      inlineDepth = 0;
    },
  });
  return { name, cells, truncated };
}

export function parseSpreadsheet(bytes: Uint8Array): SpreadsheetPreview {
  const parts = extractXMLParts(bytes);
  const budget = { elements: 0 };
  const workbookPart = officeDocumentPart(parts, budget);
  const workbookRelationshipsPart = relationshipPart(workbookPart);
  const relationships = parseRelationships(
    partText(parts, workbookRelationshipsPart),
    workbookRelationshipsPart,
    budget,
  );
  const sharedRelationship = relationships.find((relationship) =>
    relationship.type.endsWith("/sharedStrings"),
  );
  const sharedStrings = sharedRelationship
    ? parseSharedStrings(
        partText(parts, resolvePart(workbookPart, sharedRelationship.target)),
        resolvePart(workbookPart, sharedRelationship.target),
        budget,
      )
    : [];
  const descriptors = parseSheetDescriptors(partText(parts, workbookPart), workbookPart, budget);
  const sheets: SpreadsheetSheet[] = [];
  const remainingCells = { value: SPREADSHEET_CELL_LIMIT };
  const remainingText = { value: SPREADSHEET_TOTAL_TEXT_LIMIT };
  const seenTargets = new Set<string>();
  for (const descriptor of descriptors.sheets) {
    const worksheetPart = relatedPart(
      workbookPart,
      relationships,
      descriptor.relationID,
      "/worksheet",
    );
    if (!worksheetPart) throw previewError("This workbook contains a malformed worksheet.");
    if (seenTargets.has(worksheetPart)) {
      throw previewError("This workbook contains duplicate worksheet relationships.");
    }
    seenTargets.add(worksheetPart);
    sheets.push(
      parseWorksheet(
        partText(parts, worksheetPart),
        worksheetPart,
        descriptor.name,
        sharedStrings,
        budget,
        remainingCells,
        remainingText,
      ),
    );
  }
  if (!sheets.length) throw previewError("This workbook has no readable worksheets.");
  return { sheets, hiddenSheets: descriptors.hiddenSheets };
}

type SlideDescriptor = { relationID: string };

function parseSlideDescriptors(
  source: string,
  part: string,
  budget: ParseBudget,
): { slides: SlideDescriptor[]; truncated: boolean } {
  const slides: SlideDescriptor[] = [];
  let truncated = false;
  parseXML(source, part, budget, {
    open: (tag) => {
      if (tag.local !== "sldId") return;
      if (slides.length >= PRESENTATION_SLIDE_LIMIT) {
        truncated = true;
        return;
      }
      const relationID = relationshipID(tag);
      if (!relationID) throw previewError("This presentation contains a malformed slide.");
      slides.push({ relationID });
    },
  });
  return { slides, truncated };
}

function parseSlide(
  source: string,
  part: string,
  number: number,
  budget: ParseBudget,
  outputBudget: PresentationOutputBudget,
): { hidden: boolean; slide: PresentationSlide } {
  const paragraphs: string[] = [];
  let hidden = false;
  let current: string | null = null;
  let textDepth = 0;
  let characters = 0;
  parseXML(source, part, budget, {
    open: (tag) => {
      if (tag.local === "sld") {
        const show = attribute(tag, "show").toLowerCase();
        hidden = show === "0" || show === "false" || show === "off";
        return;
      }
      if (hidden) return;
      if (tag.local === "p") {
        if (paragraphs.length >= PRESENTATION_PARAGRAPH_LIMIT) {
          throw previewError("A slide contains too many paragraphs to preview safely.");
        }
        if (outputBudget.paragraphs >= PRESENTATION_TOTAL_PARAGRAPH_LIMIT) {
          outputBudget.exhausted = true;
          outputBudget.truncated = true;
          current = null;
          return;
        }
        current = "";
      } else if (tag.local === "t" && current !== null) {
        textDepth += 1;
      }
    },
    text: (text) => {
      if (hidden || current === null || textDepth === 0) return;
      const available = Math.min(
        PRESENTATION_TEXT_LIMIT - characters,
        PRESENTATION_TOTAL_TEXT_LIMIT - outputBudget.text,
      );
      if (available <= 0) {
        outputBudget.exhausted = true;
        outputBudget.truncated = true;
        return;
      }
      const boundedText = text.slice(0, available);
      current += boundedText;
      characters += boundedText.length;
      outputBudget.text += boundedText.length;
      if (boundedText.length < text.length) {
        outputBudget.exhausted = true;
        outputBudget.truncated = true;
      }
    },
    close: (tag) => {
      if (hidden) return;
      if (tag.local === "t" && current !== null) {
        textDepth -= 1;
      } else if (tag.local === "p" && current !== null) {
        if (current) {
          paragraphs.push(current);
          outputBudget.paragraphs += 1;
          if (outputBudget.paragraphs >= PRESENTATION_TOTAL_PARAGRAPH_LIMIT) {
            outputBudget.exhausted = true;
            outputBudget.truncated = true;
          }
        }
        current = null;
        textDepth = 0;
      }
    },
  });
  return {
    hidden,
    slide: { title: paragraphs[0] || `Slide ${number}`, paragraphs },
  };
}

export function parsePresentation(bytes: Uint8Array): PresentationPreview {
  const parts = extractXMLParts(bytes);
  const budget = { elements: 0 };
  const presentationPart = officeDocumentPart(parts, budget);
  const presentationRelationshipsPart = relationshipPart(presentationPart);
  const relationships = parseRelationships(
    partText(parts, presentationRelationshipsPart),
    presentationRelationshipsPart,
    budget,
  );
  const descriptors = parseSlideDescriptors(
    partText(parts, presentationPart),
    presentationPart,
    budget,
  );
  const slides: PresentationSlide[] = [];
  let hiddenSlides = 0;
  const seenTargets = new Set<string>();
  const outputBudget: PresentationOutputBudget = {
    paragraphs: 0,
    text: 0,
    exhausted: false,
    truncated: false,
  };
  for (const [descriptorIndex, descriptor] of descriptors.slides.entries()) {
    if (outputBudget.exhausted) break;
    const slidePart = relatedPart(presentationPart, relationships, descriptor.relationID, "/slide");
    if (!slidePart) throw previewError("This presentation contains a malformed slide.");
    if (seenTargets.has(slidePart)) {
      throw previewError("This presentation contains duplicate slide relationships.");
    }
    seenTargets.add(slidePart);
    const result = parseSlide(
      partText(parts, slidePart),
      slidePart,
      descriptorIndex + 1,
      budget,
      outputBudget,
    );
    if (result.hidden) hiddenSlides += 1;
    else slides.push(result.slide);
  }
  if (!slides.length) throw previewError("This presentation has no readable slides.");
  return {
    slides,
    hiddenSlides,
    truncated: descriptors.truncated || outputBudget.truncated,
  };
}
