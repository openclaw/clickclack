import DOMPurify from "dompurify";
import { marked } from "marked";

export function markdown(body: string) {
  return DOMPurify.sanitize(marked.parse(body, { async: false, breaks: true, gfm: true }));
}

export function time(value: string) {
  return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(
    new Date(value),
  );
}
