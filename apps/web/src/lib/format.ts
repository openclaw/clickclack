import DOMPurify from "dompurify";
import { marked, Renderer } from "marked";

const markdownRenderer = new Renderer();
const renderTable = markdownRenderer.table.bind(markdownRenderer);
markdownRenderer.table = (token) =>
  `<div class="markdown-table-scroll" role="region" aria-label="Scrollable Markdown table" tabindex="0">${renderTable(token)}</div>`;

export function markdown(body: string) {
  return DOMPurify.sanitize(
    marked.parse(body, { async: false, breaks: true, gfm: true, renderer: markdownRenderer }),
  );
}

export function time(value: string) {
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(
    new Date(value),
  );
}
