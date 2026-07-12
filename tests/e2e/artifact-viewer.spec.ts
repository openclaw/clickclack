import { expect, test, type Page } from "@playwright/test";
import { deflateRawSync } from "node:zlib";
import { classifyArtifact } from "../../apps/web/src/lib/artifacts";
import { parseSpreadsheet } from "../../apps/web/src/lib/office";
import {
  assertSafePDFCanvas,
  PDF_CANVAS_DIMENSION_LIMIT,
  PDF_CANVAS_PIXEL_LIMIT,
} from "../../apps/web/src/lib/pdf";
import type { Upload } from "../../apps/web/src/lib/types";

type Fixture = { filename: string; contentType: string; body: Buffer };

function storedZip(files: Record<string, string | Buffer>): Buffer {
  const local: Buffer[] = [];
  const central: Buffer[] = [];
  let offset = 0;
  for (const [name, contents] of Object.entries(files)) {
    const filename = Buffer.from(name);
    const body = Buffer.from(contents);
    const localHeader = Buffer.alloc(30);
    localHeader.writeUInt32LE(0x04034b50, 0);
    localHeader.writeUInt16LE(20, 4);
    localHeader.writeUInt32LE(body.length, 18);
    localHeader.writeUInt32LE(body.length, 22);
    localHeader.writeUInt16LE(filename.length, 26);
    local.push(localHeader, filename, body);
    const centralHeader = Buffer.alloc(46);
    centralHeader.writeUInt32LE(0x02014b50, 0);
    centralHeader.writeUInt16LE(20, 4);
    centralHeader.writeUInt16LE(20, 6);
    centralHeader.writeUInt32LE(body.length, 20);
    centralHeader.writeUInt32LE(body.length, 24);
    centralHeader.writeUInt16LE(filename.length, 28);
    centralHeader.writeUInt32LE(offset, 42);
    central.push(centralHeader, filename);
    offset += localHeader.length + filename.length + body.length;
  }
  const centralSize = central.reduce((total, part) => total + part.length, 0);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(Object.keys(files).length, 8);
  end.writeUInt16LE(Object.keys(files).length, 10);
  end.writeUInt32LE(centralSize, 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...local, ...central, end]);
}

function workbookFixture(): Buffer {
  return storedZip({
    "xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Forecast" r:id="rId1"/><sheet name="Assumptions" r:id="rId2"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/></Relationships>`,
    "xl/sharedStrings.xml": `<sst><si><t>Quarter</t></si><si><t>Revenue</t></si><si><t>Q1</t></si><si><t>Growth rate</t></si></sst>`,
    "xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row><c r="A2" t="s"><v>2</v></c><c r="B2"><v>125000</v></c></row></sheetData></worksheet>`,
    "xl/worksheets/sheet2.xml": `<worksheet><sheetData><row><c r="A1" t="s"><v>3</v></c><c r="B1"><v>0.18</v></c></row></sheetData></worksheet>`,
  });
}

function presentationFixture(): Buffer {
  return storedZip({
    "ppt/presentation.xml": `<presentation xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sldIdLst><sldId r:id="rId1"/><sldId r:id="rId2"/></sldIdLst></presentation>`,
    "ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId1" Target="slides/slide1.xml"/><Relationship Id="rId2" Target="slides/slide2.xml"/></Relationships>`,
    "ppt/slides/slide1.xml": `<sld><sp><txBody><p><r><t>Launch plan</t></r></p><p><r><t>Three milestones for a calm rollout</t></r></p></txBody></sp></sld>`,
    "ppt/slides/slide2.xml": `<sld><sp><txBody><p><r><t>Next steps</t></r></p><p><r><t>Invite the pilot team</t></r></p></txBody></sp></sld>`,
  });
}

function absoluteRelationshipWorkbook(): Buffer {
  return storedZip({
    "xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Root relative" r:id="rId1"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="/xl/worksheets/sheet1.xml"/></Relationships>`,
    "xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c r="A1" t="inlineStr"><is><t>Resolved from package root</t></is></c></row></sheetData></worksheet>`,
  });
}

function worksheetFloodWorkbook(): Buffer {
  const sheets = Array.from(
    { length: 101 },
    (_, index) => `<sheet name="Sheet ${index + 1}" r:id="rId1"/>`,
  ).join("");
  return storedZip({
    "xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>${sheets}</sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
    "xl/worksheets/sheet1.xml": `<worksheet><sheetData/></worksheet>`,
  });
}

function xmlElementFloodWorkbook(): Buffer {
  const elements = "<x/>".repeat(25_001);
  return storedZip({
    "xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Structural flood" r:id="rId1"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
    "xl/worksheets/sheet1.xml": `<worksheet><sheetData>${elements}</sheetData></worksheet>`,
  });
}

function omittedReferencesWorkbook(): Buffer {
  return storedZip({
    "xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Derived coordinates" r:id="rId1"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
    "xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="3"><c t="inlineStr"><is><t>Derived A3</t></is></c><c t="inlineStr"><is><t>Derived B3</t></is></c></row></sheetData></worksheet>`,
  });
}

function sharedStringFloodWorkbook(): Buffer {
  const strings = "<si><t>x</t></si>".repeat(10_001);
  return storedZip({
    "xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Flood" r:id="rId1"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
    "xl/sharedStrings.xml": `<sst>${strings}</sst>`,
    "xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c r="A1" t="s"><v>0</v></c></row></sheetData></worksheet>`,
  });
}

function paragraphFloodPresentation(): Buffer {
  const paragraphs = "<p><r><t>x</t></r></p>".repeat(2_001);
  return storedZip({
    "ppt/presentation.xml": `<presentation xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sldIdLst><sldId r:id="rId1"/></sldIdLst></presentation>`,
    "ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId1" Target="slides/slide1.xml"/></Relationships>`,
    "ppt/slides/slide1.xml": `<sld><sp><txBody>${paragraphs}</txBody></sp></sld>`,
  });
}

function workbookWithCellsPerSheet(count: number): Buffer {
  const cells = (column: string) =>
    Array.from(
      { length: count },
      (_, index) => `<c r="${column}${index + 1}"><v>${index}</v></c>`,
    ).join("");
  return storedZip({
    "xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="First" r:id="rId1"/><sheet name="Second" r:id="rId2"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/></Relationships>`,
    "xl/worksheets/sheet1.xml": `<worksheet><sheetData>${cells("A")}</sheetData></worksheet>`,
    "xl/worksheets/sheet2.xml": `<worksheet><sheetData>${cells("B")}</sheetData></worksheet>`,
  });
}

function presentationWithUnusedLargeMedia(): Buffer {
  return storedZip({
    "ppt/presentation.xml": `<presentation xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sldIdLst><sldId r:id="rId1"/></sldIdLst></presentation>`,
    "ppt/_rels/presentation.xml.rels": `<Relationships><Relationship Id="rId1" Target="slides/slide1.xml"/></Relationships>`,
    "ppt/slides/slide1.xml": `<sld><sp><txBody><p><r><t>Text-only preview survives media</t></r></p></txBody></sp></sld>`,
    "ppt/media/unused-video.bin": Buffer.alloc(4 * 1024 * 1024 + 1, 0x61),
  });
}

function sparseWorkbookFixture(): Buffer {
  return storedZip({
    "xl/workbook.xml": `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sparse" r:id="rId1"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`,
    "xl/worksheets/sheet1.xml": `<worksheet><sheetData><c r="A1" t="inlineStr"><is><t>Origin</t></is></c><c r="CV1000"><v>999</v></c></sheetData></worksheet>`,
  });
}

function lyingWorkbookEntry(compression: 0 | 8): Buffer {
  const filename = Buffer.from("xl/workbook.xml");
  const expanded = Buffer.alloc(2 * 1024 * 1024, 0x61);
  const body = compression === 8 ? deflateRawSync(expanded) : expanded;
  const local = Buffer.alloc(30);
  local.writeUInt32LE(0x04034b50, 0);
  local.writeUInt16LE(20, 4);
  local.writeUInt16LE(compression, 8);
  local.writeUInt32LE(body.length, 18);
  local.writeUInt32LE(1, 22);
  local.writeUInt16LE(filename.length, 26);
  const central = Buffer.alloc(46);
  central.writeUInt32LE(0x02014b50, 0);
  central.writeUInt16LE(20, 4);
  central.writeUInt16LE(20, 6);
  central.writeUInt16LE(compression, 10);
  central.writeUInt32LE(body.length, 20);
  central.writeUInt32LE(1, 24);
  central.writeUInt16LE(filename.length, 28);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(1, 8);
  end.writeUInt16LE(1, 10);
  end.writeUInt32LE(central.length + filename.length, 12);
  end.writeUInt32LE(local.length + filename.length + body.length, 16);
  return Buffer.concat([local, filename, body, central, filename, end]);
}

function uploadShape(filename: string, contentType: string): Upload {
  return {
    id: "upl_test",
    workspace_id: "wsp_test",
    owner_id: "usr_test",
    filename,
    content_type: contentType,
    byte_size: 1,
    created_at: new Date(0).toISOString(),
  };
}

function minimalPDF(): Buffer {
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R 6 0 R] /Count 2 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 180] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
    "<< /Length 52 >>\nstream\nBT /F1 18 Tf 36 100 Td (Artifact PDF proof) Tj ET\nendstream",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 180] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R >>",
    "<< /Length 57 >>\nstream\nBT /F1 18 Tf 36 100 Td (Artifact PDF page two) Tj ET\nendstream",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((object, index) => {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  pdf += offsets
    .slice(1)
    .map((offset) => `${offset.toString().padStart(10, "0")} 00000 n \n`)
    .join("");
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(pdf);
}

function oversizedPagePDF(): Buffer {
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 20000 20000] /Contents 4 0 R >>",
    "<< /Length 0 >>\nstream\n\nendstream",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((object, index) => {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  pdf += offsets
    .slice(1)
    .map((offset) => `${offset.toString().padStart(10, "0")} 00000 n \n`)
    .join("");
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(pdf);
}

function blankPagePDF(): Buffer {
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 180] /Contents 4 0 R >>",
    "<< /Length 0 >>\nstream\n\nendstream",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((object, index) => {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  pdf += offsets
    .slice(1)
    .map((offset) => `${offset.toString().padStart(10, "0")} 00000 n \n`)
    .join("");
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(pdf);
}

function oversizedImagePDF(): Buffer {
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 180] /Resources << /XObject << /Im0 5 0 R >> >> /Contents 4 0 R >>",
    "<< /Length 29 >>\nstream\nq 100 0 0 100 0 0 cm /Im0 Do Q\nendstream",
    "<< /Type /XObject /Subtype /Image /Width 5000 /Height 5000 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Length 1 >>\nstream\n0\nendstream",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  objects.forEach((object, index) => {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${index + 1} 0 obj\n${object}\nendobj\n`;
  });
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`;
  pdf += offsets
    .slice(1)
    .map((offset) => `${offset.toString().padStart(10, "0")} 00000 n \n`)
    .join("");
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return Buffer.from(pdf);
}

async function seedArtifacts(page: Page, fixtures: Fixture[]) {
  const workspaceResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspaceResponse.json()) as { workspaces: { id: string }[] };
  const workspaceID = workspaces[0].id;
  const name = `zz-artifacts-${Date.now()}`;
  const channelResponse = await page.request.post(`/api/workspaces/${workspaceID}/channels`, {
    data: { name, kind: "public" },
  });
  const { channel } = (await channelResponse.json()) as { channel: { id: string; name: string } };
  const messages: Record<string, string> = {};
  const uploads: Record<string, string> = {};

  for (const fixture of fixtures) {
    const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
      data: { body: `Viewer fixture: ${fixture.filename}` },
    });
    expect(messageResponse.ok()).toBe(true);
    const { message } = (await messageResponse.json()) as { message: { id: string } };
    const uploadResponse = await page.request.post(`/api/uploads?workspace_id=${workspaceID}`, {
      multipart: {
        file: { name: fixture.filename, mimeType: fixture.contentType, buffer: fixture.body },
      },
    });
    expect(uploadResponse.ok()).toBe(true);
    const { upload } = (await uploadResponse.json()) as { upload: { id: string } };
    const attachResponse = await page.request.post(`/api/messages/${message.id}/attachments`, {
      data: { upload_id: upload.id },
    });
    expect(attachResponse.ok()).toBe(true);
    messages[fixture.filename] = message.id;
    uploads[fixture.filename] = upload.id;
  }

  return { channel, messages, uploads };
}

test("classifies artifacts by filename and original MIME metadata", () => {
  expect(classifyArtifact(uploadShape("README.md", "application/octet-stream"))).toBe("markdown");
  expect(classifyArtifact(uploadShape("worker.ts", "text/plain"))).toBe("code");
  expect(classifyArtifact(uploadShape("page.html", "application/octet-stream"))).toBe("html");
  expect(classifyArtifact(uploadShape("report.pdf", "application/octet-stream"))).toBe("pdf");
  expect(classifyArtifact(uploadShape("forecast.xlsx", "application/octet-stream"))).toBe(
    "spreadsheet",
  );
  expect(classifyArtifact(uploadShape("launch.pptx", "application/octet-stream"))).toBe(
    "presentation",
  );
  expect(classifyArtifact(uploadShape("brief.docx", "application/octet-stream"))).toBe("docx");
  expect(classifyArtifact(uploadShape("spoofed.docx", "text/html"))).toBe("docx");
  expect(classifyArtifact(uploadShape("spoofed.docx", "application/pdf"))).toBe("docx");
  expect(
    classifyArtifact(
      uploadShape(
        "spoofed.html",
        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      ),
    ),
  ).toBe("docx");
  expect(classifyArtifact(uploadShape("notes.log", "application/octet-stream"))).toBe("text");
  expect(classifyArtifact(uploadShape("archive.zip", "application/zip"))).toBe("unsupported");
});

test("stops Office entries when output exceeds the declared size", async () => {
  for (const compression of [0, 8] as const) {
    await expect(
      parseSpreadsheet(
        new Uint8Array(lyingWorkbookEntry(compression)),
        new AbortController().signal,
      ),
    ).rejects.toThrow("exceeded its declared safe size");
  }
});

test("opens spreadsheets and slide decks with navigation", async ({ page }) => {
  const { channel } = await seedArtifacts(page, [
    {
      filename: "forecast.xlsx",
      contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      body: workbookFixture(),
    },
    {
      filename: "launch.pptx",
      contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
      body: presentationFixture(),
    },
    {
      filename: "sparse.xlsx",
      contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      body: sparseWorkbookFixture(),
    },
    {
      filename: "absolute.xlsx",
      contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      body: absoluteRelationshipWorkbook(),
    },
    {
      filename: "many-sheets.xlsx",
      contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      body: worksheetFloodWorkbook(),
    },
    {
      filename: "xml-element-flood.xlsx",
      contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      body: xmlElementFloodWorkbook(),
    },
    {
      filename: "cell-budget.xlsx",
      contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      body: workbookWithCellsPerSheet(6_000),
    },
    {
      filename: "omitted-references.xlsx",
      contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      body: omittedReferencesWorkbook(),
    },
    {
      filename: "shared-string-flood.xlsx",
      contentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
      body: sharedStringFloodWorkbook(),
    },
    {
      filename: "paragraph-flood.pptx",
      contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
      body: paragraphFloodPresentation(),
    },
    {
      filename: "media-heavy.pptx",
      contentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
      body: presentationWithUnusedLargeMedia(),
    },
  ]);
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });

  await page.getByRole("button", { name: "Open forecast.xlsx" }).click();
  await expect(viewer.getByRole("region", { name: "Forecast worksheet" })).toContainText("125000");
  if (process.env.CAPTURE_OFFICE_PROOF) {
    await viewer.screenshot({ path: "docs/proof/artifact-viewer-spreadsheet.png" });
  }
  await viewer.getByRole("tab", { name: "Assumptions" }).click();
  await expect(viewer.getByRole("region", { name: "Assumptions worksheet" })).toContainText("0.18");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open absolute.xlsx" }).click();
  await expect(viewer.getByText("Resolved from package root")).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open many-sheets.xlsx" }).click();
  await expect(viewer.getByRole("alert")).toContainText("too many worksheets");
  if (process.env.CAPTURE_OFFICE_PROOF) {
    await page.screenshot({
      path: "docs/proof/artifact-viewer-office-limit-fallback.png",
      fullPage: true,
    });
  }
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open xml-element-flood.xlsx" }).click();
  await expect(viewer.getByRole("alert")).toContainText("too many XML elements");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open cell-budget.xlsx" }).click();
  await viewer.getByRole("tab", { name: "Second" }).click();
  await expect(viewer.getByText("Preview is limited to 10,000 cells")).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open omitted-references.xlsx" }).click();
  await expect(viewer.locator("tbody tr").nth(2)).toContainText("Derived A3");
  await expect(viewer.locator("tbody tr").nth(2)).toContainText("Derived B3");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open shared-string-flood.xlsx" }).click();
  await expect(viewer.getByRole("alert")).toContainText("too many shared strings");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open paragraph-flood.pptx" }).click();
  await expect(viewer.getByRole("alert")).toContainText("too many paragraphs");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open media-heavy.pptx" }).click();
  await expect(viewer.getByText("Text-only preview survives media")).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open sparse.xlsx" }).click();
  await expect(viewer.getByText("Preview is limited to 10,000 cells")).toBeVisible();
  await expect(viewer.locator("tbody td")).toHaveCount(10_000);
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open launch.pptx" }).click();
  await expect(viewer.getByLabel("Slide 1: Launch plan")).toContainText("Three milestones");
  if (process.env.CAPTURE_OFFICE_PROOF) {
    await viewer.screenshot({ path: "docs/proof/artifact-viewer-slide-deck.png" });
  }
  await viewer.getByRole("button", { name: "Next" }).click();
  await expect(viewer.getByLabel("Slide 2: Next steps")).toContainText("Invite the pilot team");
  await expect(viewer.getByRole("link", { name: "Download launch.pptx" })).toBeVisible();
});

test("bounds PDF canvas dimensions and total backing pixels", () => {
  expect(() => assertSafePDFCanvas(4_096, 4_096)).not.toThrow();
  expect(() => assertSafePDFCanvas(PDF_CANVAS_DIMENSION_LIMIT + 1, 1)).toThrow(
    "too large to preview safely",
  );
  expect(() => assertSafePDFCanvas(PDF_CANVAS_PIXEL_LIMIT / 4_096 + 1, 4_096)).toThrow(
    "too large to preview safely",
  );
});

test("opens safe code, Markdown, PDF, and HTML previews with DOCX download-only", async ({
  page,
}) => {
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  let externalRequests = 0;
  page.on("request", (request) => {
    if (request.url().startsWith("https://artifact-proof.invalid/")) externalRequests += 1;
  });
  const fixtures: Fixture[] = [
    {
      filename: "viewer-proof.ts",
      contentType: "text/typescript",
      body: Buffer.from("const proof: string = 'highlighted';\nconsole.log(proof);\n"),
    },
    {
      filename: "viewer-proof.md",
      contentType: "text/markdown",
      body: Buffer.from(
        '# Markdown artifact\n\n**Safe preview**\n\n[external](https://artifact-proof.invalid/markdown-nav)\n\n![leak](https://artifact-proof.invalid/markdown-image)\n\n<img alt="srcset leak" srcset="https://artifact-proof.invalid/srcset 1x"><video poster="https://artifact-proof.invalid/poster"></video><div data-css-leak style="background:url(https://artifact-proof.invalid/style)">styled</div><style>@import "https://artifact-proof.invalid/import.css"</style><table background="https://artifact-proof.invalid/background-attribute"><tr><td>legacy background</td></tr></table>\n\n<script>window.parent.__artifactScriptRan = true</script>',
      ),
    },
    { filename: "viewer-proof.pdf", contentType: "application/pdf", body: minimalPDF() },
    {
      filename: "viewer-proof.docx",
      contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      body: Buffer.from("DOCX bytes are never parsed in the browser"),
    },
    {
      filename: "viewer-proof.html",
      contentType: "text/html",
      body: Buffer.from(
        '<!doctype html><html><head title="&quot;>&quot;"><meta http-equiv="refresh" content="0;url=https://artifact-proof.invalid/refresh"><style>@\\69mport "https://artifact-proof.invalid/escaped-import.css"; @import "https://artifact-proof.invalid/import.css"; h1{color:teal;background:url(https://artifact-proof.invalid/background.png)}</style></head><body><h1 style="background:url(https://artifact-proof.invalid/inline-style.png)">Sandboxed web artifact</h1><a href="https://artifact-proof.invalid/navigate">external link</a><a data-data-navigation href="data:text/html,%3Cimg%20src%3D%22https%3A%2F%2Fartifact-proof.invalid%2Fdata-navigation.png%22%3E">data navigation</a><table background="https://artifact-proof.invalid/legacy-background.png"><tr><td>legacy background</td></tr></table><img src="https://artifact-proof.invalid/leak.png"><form action="https://artifact-proof.invalid/submit"><button>submit</button></form><iframe src="https://artifact-proof.invalid/frame"></iframe><script>window.parent.__artifactScriptRan = true</script></body></html>',
      ),
    },
  ];
  const { channel } = await seedArtifacts(page, fixtures);

  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();

  await page.getByRole("button", { name: "Open viewer-proof.ts" }).click();
  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer).toBeVisible();
  await expect(viewer.locator(".hljs-keyword")).toContainText("const");
  await page.keyboard.press("Escape");
  await expect(viewer).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Open viewer-proof.ts" })).toBeFocused();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open viewer-proof.md" }).click();
  const markdownPreview = viewer.locator(".artifact-viewer__markdown");
  await expect(markdownPreview.getByRole("heading", { name: "Markdown artifact" })).toBeVisible();
  await expect(markdownPreview.locator("script")).toHaveCount(0);
  await expect(markdownPreview.getByText("external")).toBeVisible();
  await expect(markdownPreview.locator("a, img, video, div, style")).toHaveCount(0);
  await expect(markdownPreview.getByText("legacy background")).toBeVisible();
  await expect(
    markdownPreview.locator("[background], [href], [poster], [src], [srcset], [style]"),
  ).toHaveCount(0);
  await expect.poll(() => externalRequests).toBe(0);
  await viewer.getByRole("button", { name: "Source" }).click();
  await expect(viewer.locator("pre")).toContainText("# Markdown artifact");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open viewer-proof.pdf" }).click();
  await expect(viewer.getByText("Page 1 of 2")).toBeVisible();
  await expect(viewer.locator("canvas")).toBeVisible();
  await expect(viewer.getByRole("region", { name: "PDF page 1 text" })).toContainText(
    "Artifact PDF proof",
  );
  await viewer.getByRole("button", { name: "Next" }).click();
  await expect(viewer.getByText("Page 2 of 2")).toBeVisible();
  const widthBeforeZoom = await viewer.locator("canvas").evaluate((canvas) => canvas.style.width);
  await viewer.getByRole("button", { name: "Zoom in" }).click();
  await expect(viewer.getByText("120%")).toBeVisible();
  await expect
    .poll(() => viewer.locator("canvas").evaluate((canvas) => canvas.style.width))
    .not.toBe(widthBeforeZoom);
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await expect(page.getByRole("link", { name: "Download viewer-proof.docx" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Open viewer-proof.docx" })).toHaveCount(0);
  expect(pageErrors).toEqual([]);

  await page.getByRole("button", { name: "Open viewer-proof.html" }).click();
  const htmlPreview = viewer.locator(".artifact-viewer__html");
  await expect(htmlPreview.getByRole("heading", { name: "Sandboxed web artifact" })).toBeVisible();
  await expect(htmlPreview.locator("script, form, iframe")).toHaveCount(0);
  await expect(htmlPreview.locator('meta[http-equiv="refresh"]')).toHaveCount(0);
  await expect(htmlPreview.getByText("external link")).not.toHaveAttribute("href");
  await expect(htmlPreview.getByText("data navigation")).not.toHaveAttribute("href");
  await htmlPreview.getByText("data navigation").click();
  await expect(htmlPreview.locator("[src]")).toHaveCount(0);
  await expect(htmlPreview.locator("[background]")).toHaveCount(0);
  await expect(htmlPreview.locator("[href], [poster], [srcset], [style]")).toHaveCount(0);
  await expect(htmlPreview.locator("style, [style]")).toHaveCount(0);
  await expect.poll(() => externalRequests).toBe(0);
  const scriptMarker = await page.evaluate(
    () => (window as Window & { __artifactScriptRan?: boolean }).__artifactScriptRan,
  );
  expect(scriptMarker).toBeUndefined();
  await viewer.getByRole("button", { name: "Source" }).click();
  await expect(viewer.locator("pre")).toContainText("Sandboxed web artifact");
  await viewer.getByRole("button", { name: "Preview" }).click();

  if (process.env.CAPTURE_ARTIFACT_PROOF === "1") {
    await page.evaluate(
      ({ requests, scriptRan }) => {
        const diagnostics = document.createElement("aside");
        diagnostics.setAttribute("data-artifact-proof", "");
        diagnostics.style.cssText =
          "position:fixed;left:24px;bottom:24px;z-index:9999;width:430px;padding:20px;border:1px solid #35516f;border-radius:12px;background:#101820;color:#eef6ff;font:14px/1.5 ui-monospace,monospace;box-shadow:0 18px 50px #0008";
        diagnostics.innerHTML = `<strong style="display:block;margin-bottom:10px;color:#7ee787">Playwright live browser diagnostics: PASS</strong>
          <div>preview isolation: inert-template sanitized fragment</div>
          <div>script execution marker: ${scriptRan ? "SET (FAIL)" : "absent"}</div>
          <div>requests to artifact-proof.invalid: ${requests}</div>
          <div>scripts/forms/frames in preview: 0 / 0 / 0</div>
          <div>external href/src/CSS references: stripped</div>`;
        document.body.append(diagnostics);
      },
      { requests: externalRequests, scriptRan: scriptMarker === true },
    );
    await page.screenshot({
      path: "docs/proof/artifact-viewer-html-isolation.png",
      fullPage: true,
    });
  }

  await page.setViewportSize({ width: 390, height: 844 });
  const mobileViewer = page.getByRole("dialog", { name: "Artifact viewer" });
  await expect(mobileViewer).toHaveAttribute("aria-modal", "true");
  await expect(page.locator(".timeline")).toHaveAttribute("inert", "");
  const closeButton = mobileViewer.getByRole("button", { name: "Close artifact viewer" });
  await closeButton.focus();
  await page.keyboard.press("Tab");
  await expect(mobileViewer.getByLabel("Artifact content")).toBeFocused();
  const bounds = await mobileViewer.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return { left: rect.left, top: rect.top, width: rect.width, height: rect.height };
  });
  expect(bounds.left).toBe(0);
  expect(bounds.top).toBe(0);
  expect(bounds.width).toBe(390);
  expect(bounds.height).toBe(844);
});

test("falls back to source before structured previews can exhaust the DOM", async ({ page }) => {
  const { channel } = await seedArtifacts(page, [
    {
      filename: "complex.html",
      contentType: "text/html",
      body: Buffer.from(`<main>${"<i>x</i>".repeat(10_100)}</main>`),
    },
    {
      filename: "complex.md",
      contentType: "text/markdown",
      body: Buffer.from(`${"- x\n".repeat(10_100)}`),
    },
    {
      filename: "sanitized-empty.md",
      contentType: "text/markdown",
      body: Buffer.from("<script>unsafe but inspectable source</script>"),
    },
  ]);
  await page.goto("/app");
  const channelHref = await page
    .getByRole("link", { name: `# ${channel.name}` })
    .getAttribute("href");
  await page.goto(channelHref!);

  await page.getByRole("button", { name: "Open complex.html" }).click();
  let viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer.locator("iframe")).toHaveCount(0);
  await expect(viewer.locator("pre")).toContainText("<main>");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open complex.md" }).click();
  viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer.locator(".artifact-viewer__markdown")).toHaveCount(0);
  await expect(viewer.locator("pre")).toContainText("- x");
  await expect(viewer.getByRole("button", { name: "Preview" })).toHaveCount(0);
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await page.getByRole("button", { name: "Open sanitized-empty.md" }).click();
  viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer.getByRole("button", { name: "Source" })).toBeVisible();
  await viewer.getByRole("button", { name: "Source" }).click();
  await expect(viewer.locator("pre")).toContainText("unsafe but inspectable source");
});

test("makes a viewer opened at the mobile breakpoint modal immediately", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const { channel } = await seedArtifacts(page, [
    {
      filename: "mobile.md",
      contentType: "text/markdown",
      body: Buffer.from("# Mobile modal"),
    },
  ]);
  await page.goto("/app");
  const channelHref = await page
    .getByRole("link", { name: `# ${channel.name}` })
    .getAttribute("href");
  await page.goto(channelHref!);
  await page.getByRole("button", { name: "Open mobile.md" }).click();

  const viewer = page.getByRole("dialog", { name: "Artifact viewer" });
  await expect(viewer).toHaveAttribute("aria-modal", "true");
  await expect(page.locator(".timeline")).toHaveAttribute("inert", "");
  await expect(page.locator(".shell > :not(.artifact-viewer)[inert]")).not.toHaveCount(0);
});

test("shows local fallbacks for oversized and malformed artifacts", async ({ page }) => {
  const pageErrors: string[] = [];
  page.on("pageerror", (error) => pageErrors.push(error.message));
  const fixtures: Fixture[] = [
    {
      filename: "oversized.txt",
      contentType: "text/plain",
      body: Buffer.alloc(2 * 1024 * 1024 + 1, 0x61),
    },
    {
      filename: "malformed.docx",
      contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      body: Buffer.from("not a DOCX package"),
    },
    {
      filename: "spoofed-media.docx",
      contentType: "image/svg+xml",
      body: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"><text>not media</text></svg>'),
    },
    {
      filename: "oversized.docx",
      contentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      body: Buffer.alloc(16 * 1024 * 1024 + 1, 0x61),
    },
    {
      filename: "malformed.pdf",
      contentType: "application/pdf",
      body: Buffer.from("not a PDF document"),
    },
    {
      filename: "blank-page.pdf",
      contentType: "application/pdf",
      body: blankPagePDF(),
    },
    {
      filename: "oversized-image.pdf",
      contentType: "application/pdf",
      body: oversizedImagePDF(),
    },
    {
      filename: "oversized-page.pdf",
      contentType: "application/pdf",
      body: oversizedPagePDF(),
    },
  ];
  const { channel } = await seedArtifacts(page, fixtures);
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });

  await page.getByRole("button", { name: "Open oversized.txt" }).click();
  await expect(viewer.getByRole("alert")).toContainText("Preview is limited to 2.0 MB");
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await expect(page.getByRole("link", { name: "Download malformed.docx" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Download spoofed-media.docx" })).toBeVisible();
  await expect(page.getByRole("img", { name: "spoofed-media.docx" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Download oversized.docx" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Open .*\.docx/ })).toHaveCount(0);

  await page.getByRole("button", { name: "Open malformed.pdf" }).click();
  await expect(viewer.getByRole("alert")).toContainText("Preview unavailable");
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open blank-page.pdf" }).click();
  await expect(viewer.locator("canvas")).toBeVisible();
  await expect(viewer.getByRole("alert")).toHaveCount(0);
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open oversized-page.pdf" }).click();
  await expect(viewer.getByRole("alert")).toContainText("too large to preview safely");
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
  await expect(viewer.locator("canvas")).toHaveCount(0);

  if (process.env.CAPTURE_PDF_LIMIT_PROOF === "1") {
    const canvasCount = await viewer.locator("canvas").count();
    await page.evaluate(
      ({ canvases, dimensionLimit, pixelLimit }) => {
        const diagnostics = document.createElement("aside");
        diagnostics.setAttribute("data-artifact-proof", "");
        diagnostics.style.cssText =
          "position:fixed;left:24px;bottom:24px;z-index:9999;width:450px;padding:20px;border:1px solid #35516f;border-radius:12px;background:#101820;color:#eef6ff;font:14px/1.5 ui-monospace,monospace;box-shadow:0 18px 50px #0008";
        diagnostics.innerHTML = `<strong style="display:block;margin-bottom:10px;color:#7ee787">Playwright live PDF diagnostics: PASS</strong>
          <div>crafted page geometry: 20,000 × 20,000 pt</div>
          <div>backing dimension limit: ${dimensionLimit.toLocaleString()} px</div>
          <div>backing pixel budget: ${(pixelLimit / 1024 / 1024).toFixed(0)} MP</div>
          <div>viewer canvases allocated: ${canvases}</div>
          <div>safe download fallback: visible</div>`;
        document.body.append(diagnostics);
      },
      {
        canvases: canvasCount,
        dimensionLimit: PDF_CANVAS_DIMENSION_LIMIT,
        pixelLimit: PDF_CANVAS_PIXEL_LIMIT,
      },
    );
    await page.screenshot({
      path: "docs/proof/artifact-viewer-pdf-canvas-limit.png",
      fullPage: true,
    });
  }

  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();
  await page.waitForTimeout(250);

  await page.getByRole("button", { name: "Open oversized-image.pdf" }).click();
  await expect.poll(() => pageErrors).toEqual([]);
  await expect(viewer.getByRole("alert")).toContainText(
    "could not be rendered completely within safety limits",
  );
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
  await expect(viewer.locator("canvas")).toHaveCount(0);
});

test("near-limit code remains interruptible and falls back to escaped source", async ({ page }) => {
  const nearLimitSource = `<script>window.__artifactCodeRan = true</script>\n${"const value = 1;\n".repeat(120_000)}`;
  const { channel } = await seedArtifacts(page, [
    {
      filename: "near-limit.ts",
      contentType: "text/typescript",
      body: Buffer.from(nearLimitSource.slice(0, 2 * 1024 * 1024)),
    },
  ]);
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();

  await page.getByRole("button", { name: "Open near-limit.ts" }).click();
  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer.locator("pre")).toContainText("window.__artifactCodeRan", { timeout: 5_000 });
  await expect(viewer.locator(".hljs-keyword")).toHaveCount(0);
  expect(
    await page.evaluate(
      () => (window as Window & { __artifactCodeRan?: boolean }).__artifactCodeRan,
    ),
  ).toBeUndefined();

  await page.keyboard.press("Escape");
  await expect(viewer).toHaveCount(0, { timeout: 1_000 });
  await expect(page.getByRole("button", { name: "Open near-limit.ts" })).toBeFocused();
});

test("enforces the actual streamed byte limit instead of trusting upload metadata", async ({
  page,
}) => {
  const { channel, uploads } = await seedArtifacts(page, [
    {
      filename: "metadata-lie.txt",
      contentType: "text/plain",
      body: Buffer.from("small stored fixture"),
    },
  ]);
  await page.route(`**/api/uploads/${uploads["metadata-lie.txt"]}`, async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "text/plain",
      body: Buffer.alloc(2 * 1024 * 1024 + 1, 0x61),
    });
  });
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await page.getByRole("button", { name: "Open metadata-lie.txt" }).click();

  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer.getByRole("alert")).toContainText("2.0 MB safety limit");
  await expect(viewer.getByRole("link", { name: "Download original" })).toBeVisible();
});

test("adds an attachment from message.updated without reloading", async ({ page }) => {
  const { channel } = await seedArtifacts(page, []);
  const messageResponse = await page.request.post(`/api/channels/${channel.id}/messages`, {
    data: { body: "Realtime artifact delivery" },
  });
  const { message } = (await messageResponse.json()) as { message: { id: string } };
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();
  await expect(page.getByText("Realtime artifact delivery")).toBeVisible();

  const workspaceResponse = await page.request.get("/api/workspaces");
  const { workspaces } = (await workspaceResponse.json()) as { workspaces: { id: string }[] };
  const uploadResponse = await page.request.post(`/api/uploads?workspace_id=${workspaces[0].id}`, {
    multipart: {
      file: {
        name: "realtime-proof.md",
        mimeType: "text/markdown",
        buffer: Buffer.from("# Delivered through message.updated"),
      },
    },
  });
  const { upload } = (await uploadResponse.json()) as { upload: { id: string } };
  await page.request.post(`/api/messages/${message.id}/attachments`, {
    data: { upload_id: upload.id },
  });

  await expect(page.getByRole("button", { name: "Open realtime-proof.md" })).toBeVisible();
});

test("returns to the routed thread after closing an artifact", async ({ page }) => {
  const { channel, messages } = await seedArtifacts(page, [
    {
      filename: "thread-proof.md",
      contentType: "text/markdown",
      body: Buffer.from("# Thread artifact"),
    },
  ]);
  await page.goto("/app");
  await page.getByRole("link", { name: `# ${channel.name}` }).click();

  const message = page.locator(`[data-message-id="${messages["thread-proof.md"]}"]`);
  const parentPath = new URL(page.url()).pathname;
  await message.getByRole("button", { name: "Open thread", exact: true }).click();
  const thread = page.getByRole("complementary", { name: "Thread pane" });
  await expect(thread).toBeVisible();
  await expect.poll(() => new URL(page.url()).pathname).not.toBe(parentPath);
  const threadPath = new URL(page.url()).pathname;

  await thread.getByRole("button", { name: "Reply" }).first().click();
  await expect(thread.getByText(/Replying to/)).toBeVisible();
  const threadScroll = thread.locator(".thread-scroll");
  await threadScroll.evaluate((element) => {
    element.setAttribute("data-mount-proof", "preserved");
    const spacer = document.createElement("div");
    spacer.style.cssText = "height:2000px;min-height:2000px;flex:none";
    element.prepend(spacer);
    element.scrollTop = element.scrollHeight;
  });
  expect(await threadScroll.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);

  await thread.getByRole("button", { name: "Open thread-proof.md" }).click();
  const viewer = page.getByRole("complementary", { name: "Artifact viewer" });
  await expect(viewer.getByRole("heading", { name: "Thread artifact" })).toBeVisible();
  await expect(page.locator(".thread")).toHaveAttribute("aria-hidden", "true");
  await viewer.getByRole("button", { name: "Close artifact viewer" }).click();

  await expect(thread).toBeVisible();
  await expect(thread.getByText(/Replying to/)).toBeVisible();
  await expect(threadScroll).toHaveAttribute("data-mount-proof", "preserved");
  expect(await threadScroll.evaluate((element) => element.scrollTop)).toBeGreaterThan(0);
  expect(new URL(page.url()).pathname).toBe(threadPath);
  await expect(thread.getByRole("button", { name: "Open thread-proof.md" })).toBeFocused();
});
