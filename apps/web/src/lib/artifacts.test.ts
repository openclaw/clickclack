import test from "node:test";
import assert from "node:assert/strict";
import {
  artifactContentType,
  artifactExtension,
  artifactKindLabel,
  artifactLanguage,
  artifactPreviewLimit,
  classifyArtifact,
  OFFICE_ARTIFACT_LIMIT,
  PDF_ARTIFACT_LIMIT,
  TEXT_ARTIFACT_LIMIT,
  type ArtifactKind,
} from "./artifacts.ts";
import type { Upload } from "./types";

const upload = (filename: string, content_type = ""): Upload =>
  ({
    id: "u1",
    workspace_id: "w1",
    owner_id: "o1",
    filename,
    content_type,
    byte_size: 1,
    created_at: "2026-01-01T00:00:00Z",
  }) as Upload;

test("artifactExtension lowercases and takes the segment after the last dot", () => {
  assert.equal(artifactExtension("Report.TS"), "ts");
  assert.equal(artifactExtension("archive.tar.gz"), "gz");
});

test("artifactExtension strips directory prefixes for both slash styles", () => {
  assert.equal(artifactExtension("a/b/notes.MD"), "md");
  assert.equal(artifactExtension("a\\b\\slides.PPTX"), "pptx");
});

test("artifactExtension returns empty for no extension, dotfiles, and trailing separators", () => {
  assert.equal(artifactExtension("README"), "");
  assert.equal(artifactExtension(".gitignore"), "");
  assert.equal(artifactExtension("nested/dir/"), "");
});

test("artifactContentType strips parameters, trims, and lowercases", () => {
  assert.equal(artifactContentType(upload("f", "text/HTML; charset=utf-8")), "text/html");
  assert.equal(artifactContentType(upload("f", "  Application/PDF  ")), "application/pdf");
});

test("artifactContentType falls back to empty when the header is blank", () => {
  assert.equal(artifactContentType(upload("f", "")), "");
});

test("classifyArtifact detects docx by extension and by content type", () => {
  assert.equal(classifyArtifact(upload("resume.docx")), "docx");
  assert.equal(
    classifyArtifact(
      upload(
        "resume",
        "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
      ),
    ),
    "docx",
  );
});

test("classifyArtifact detects spreadsheets by extension and by content type", () => {
  assert.equal(classifyArtifact(upload("data.xlsm")), "spreadsheet");
  assert.equal(
    classifyArtifact(
      upload("data", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"),
    ),
    "spreadsheet",
  );
});

test("classifyArtifact detects presentations by extension and by content type", () => {
  assert.equal(classifyArtifact(upload("deck.ppsx")), "presentation");
  assert.equal(
    classifyArtifact(
      upload("deck", "application/vnd.openxmlformats-officedocument.presentationml.slideshow"),
    ),
    "presentation",
  );
});

test("classifyArtifact detects markdown, html, and pdf by extension and by content type", () => {
  assert.equal(classifyArtifact(upload("notes.markdown")), "markdown");
  assert.equal(classifyArtifact(upload("notes", "text/markdown")), "markdown");
  assert.equal(classifyArtifact(upload("page.htm")), "html");
  assert.equal(classifyArtifact(upload("page", "text/html")), "html");
  assert.equal(classifyArtifact(upload("paper.pdf")), "pdf");
  assert.equal(classifyArtifact(upload("paper", "application/pdf")), "pdf");
});

test("classifyArtifact detects code by known extension and by known content type", () => {
  assert.equal(classifyArtifact(upload("main.rs")), "code");
  assert.equal(classifyArtifact(upload("blob", "application/json")), "code");
});

test("classifyArtifact detects text by extension, plain type, and the text/* fallback", () => {
  assert.equal(classifyArtifact(upload("server.log")), "text");
  assert.equal(classifyArtifact(upload("note", "text/plain")), "text");
  assert.equal(classifyArtifact(upload("note", "text/rtf")), "text");
});

test("classifyArtifact returns unsupported for unknown extension and content type", () => {
  assert.equal(classifyArtifact(upload("photo.png", "image/png")), "unsupported");
  assert.equal(classifyArtifact(upload("blob")), "unsupported");
});

test("classifyArtifact lets the extension win over the content type in the cascade", () => {
  assert.equal(classifyArtifact(upload("notes.md", "application/pdf")), "markdown");
});

test("artifactLanguage maps known code extensions", () => {
  assert.equal(artifactLanguage(upload("app.tsx")), "typescript");
  assert.equal(artifactLanguage(upload("run.py")), "python");
});

test("artifactLanguage falls back to content-type substrings when the extension is unknown", () => {
  assert.equal(artifactLanguage(upload("blob", "application/ld+json")), "json");
  assert.equal(artifactLanguage(upload("blob", "text/javascript")), "javascript");
  assert.equal(artifactLanguage(upload("blob", "application/typescript")), "typescript");
  assert.equal(artifactLanguage(upload("blob", "application/x-yaml")), "yaml");
  assert.equal(artifactLanguage(upload("blob", "text/xml")), "xml");
  assert.equal(artifactLanguage(upload("blob", "application/sql")), "sql");
});

test("artifactLanguage prefers the code extension over the content type", () => {
  assert.equal(artifactLanguage(upload("run.py", "application/json")), "python");
});

test("artifactLanguage returns undefined when nothing matches", () => {
  assert.equal(artifactLanguage(upload("photo.png", "image/png")), undefined);
});

test("artifactKindLabel returns a label for every kind and a default", () => {
  const labels: Record<ArtifactKind, string> = {
    code: "Code",
    text: "Text",
    markdown: "Markdown",
    pdf: "PDF",
    docx: "Word document",
    spreadsheet: "Spreadsheet",
    presentation: "Slide text outline",
    html: "Web page",
    unsupported: "File",
  };
  for (const [kind, label] of Object.entries(labels)) {
    assert.equal(artifactKindLabel(kind as ArtifactKind), label);
  }
});

test("artifactPreviewLimit returns the per-family cap", () => {
  assert.equal(artifactPreviewLimit("pdf"), PDF_ARTIFACT_LIMIT);
  assert.equal(artifactPreviewLimit("spreadsheet"), OFFICE_ARTIFACT_LIMIT);
  assert.equal(artifactPreviewLimit("presentation"), OFFICE_ARTIFACT_LIMIT);
  for (const kind of ["code", "text", "markdown", "html"] as ArtifactKind[]) {
    assert.equal(artifactPreviewLimit(kind), TEXT_ARTIFACT_LIMIT);
  }
});

test("artifactPreviewLimit returns undefined for kinds without an inline preview", () => {
  assert.equal(artifactPreviewLimit("docx"), undefined);
  assert.equal(artifactPreviewLimit("unsupported"), undefined);
});
