import type { Upload } from "./types";

export type ArtifactKind =
  | "code"
  | "text"
  | "markdown"
  | "pdf"
  | "docx"
  | "spreadsheet"
  | "presentation"
  | "html"
  | "unsupported";

export const TEXT_ARTIFACT_LIMIT = 2 * 1024 * 1024;
export const PDF_ARTIFACT_LIMIT = 64 * 1024 * 1024;
export const OFFICE_ARTIFACT_LIMIT = 24 * 1024 * 1024;

const CODE_LANGUAGES: Record<string, string> = {
  bash: "bash",
  c: "c",
  cc: "cpp",
  cpp: "cpp",
  css: "css",
  go: "go",
  h: "c",
  hpp: "cpp",
  java: "java",
  js: "javascript",
  json: "json",
  jsx: "javascript",
  mjs: "javascript",
  py: "python",
  rb: "ruby",
  rs: "rust",
  sh: "bash",
  sql: "sql",
  toml: "ini",
  ts: "typescript",
  tsx: "typescript",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
  zsh: "bash",
};

const CODE_CONTENT_TYPES = new Set([
  "application/javascript",
  "application/json",
  "application/ld+json",
  "application/sql",
  "application/toml",
  "application/typescript",
  "application/xml",
  "application/x-httpd-php",
  "application/x-javascript",
  "application/x-sh",
  "application/x-yaml",
  "text/css",
  "text/csv",
  "text/javascript",
  "text/typescript",
  "text/x-python",
  "text/x-shellscript",
  "text/xml",
  "text/yaml",
]);

export function artifactExtension(filename: string): string {
  const basename = filename.toLowerCase().split(/[\\/]/).pop() || "";
  const dot = basename.lastIndexOf(".");
  return dot > 0 ? basename.slice(dot + 1) : "";
}

export function artifactContentType(upload: Upload): string {
  return (upload.content_type || "").split(";", 1)[0].trim().toLowerCase();
}

export function classifyArtifact(upload: Upload): ArtifactKind {
  const extension = artifactExtension(upload.filename);
  const contentType = artifactContentType(upload);

  if (
    extension === "docx" ||
    contentType === "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
  ) {
    return "docx";
  }
  if (
    ["xlsx", "xlsm", "xltx", "xltm"].includes(extension) ||
    contentType === "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" ||
    contentType === "application/vnd.ms-excel.sheet.macroenabled.12"
  )
    return "spreadsheet";
  if (
    ["pptx", "pptm", "potx", "potm", "ppsx", "ppsm"].includes(extension) ||
    contentType === "application/vnd.openxmlformats-officedocument.presentationml.presentation" ||
    contentType === "application/vnd.ms-powerpoint.presentation.macroenabled.12"
  )
    return "presentation";
  if (extension === "md" || extension === "markdown" || contentType === "text/markdown")
    return "markdown";
  if (extension === "html" || extension === "htm" || contentType === "text/html") return "html";
  if (extension === "pdf" || contentType === "application/pdf") return "pdf";
  if (CODE_LANGUAGES[extension] || CODE_CONTENT_TYPES.has(contentType)) return "code";
  if (extension === "txt" || extension === "log" || contentType === "text/plain") return "text";
  if (contentType.startsWith("text/")) return "text";
  return "unsupported";
}

export function artifactLanguage(upload: Upload): string | undefined {
  const extension = artifactExtension(upload.filename);
  if (CODE_LANGUAGES[extension]) return CODE_LANGUAGES[extension];
  const contentType = artifactContentType(upload);
  if (contentType.includes("json")) return "json";
  if (contentType.includes("javascript")) return "javascript";
  if (contentType.includes("typescript")) return "typescript";
  if (contentType.includes("yaml")) return "yaml";
  if (contentType.includes("xml")) return "xml";
  if (contentType.includes("sql")) return "sql";
  return undefined;
}

export function artifactKindLabel(kind: ArtifactKind): string {
  switch (kind) {
    case "code":
      return "Code";
    case "text":
      return "Text";
    case "markdown":
      return "Markdown";
    case "pdf":
      return "PDF";
    case "docx":
      return "Word document";
    case "spreadsheet":
      return "Spreadsheet";
    case "presentation":
      return "Slide deck";
    case "html":
      return "Web page";
    default:
      return "File";
  }
}

export function artifactPreviewLimit(kind: ArtifactKind): number | undefined {
  if (kind === "pdf") return PDF_ARTIFACT_LIMIT;
  if (kind === "spreadsheet" || kind === "presentation") return OFFICE_ARTIFACT_LIMIT;
  if (kind === "code" || kind === "text" || kind === "markdown" || kind === "html") {
    return TEXT_ARTIFACT_LIMIT;
  }
  return undefined;
}
