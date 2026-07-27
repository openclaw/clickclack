import assert from "node:assert/strict";
import test from "node:test";
import { resolveEmbedHostOrigin, resolveEmbedThemeMode } from "./embed-theme.ts";

test("accepts explicit light and dark modes on channel and thread embeds", () => {
  assert.equal(
    resolveEmbedThemeMode({ pathname: "/embed/channel/T1/C1", search: "?theme=light" }),
    "light",
  );
  assert.equal(
    resolveEmbedThemeMode({ pathname: "/embed/thread/T1/M1", search: "?theme=dark" }),
    "dark",
  );
});

test("does not apply embed themes to normal ClickClack pages", () => {
  assert.equal(resolveEmbedThemeMode({ pathname: "/app/T1/C1", search: "?theme=dark" }), null);
  assert.equal(resolveEmbedThemeMode({ pathname: "/embed/channel/T1/C1", search: "" }), null);
  assert.equal(
    resolveEmbedThemeMode({ pathname: "/embed/channel/T1/C1", search: "?theme=system" }),
    null,
  );
});

test("accepts only exact HTTP and HTTPS host origins on embed routes", () => {
  assert.equal(
    resolveEmbedHostOrigin({
      pathname: "/embed/channel/T1/C1",
      search: "?hostOrigin=https%3A%2F%2Fcontrol.example.com",
    }),
    "https://control.example.com",
  );
  assert.equal(
    resolveEmbedHostOrigin({
      pathname: "/embed/thread/T1/M1",
      search: "?hostOrigin=http%3A%2F%2Flocalhost%3A18789",
    }),
    "http://localhost:18789",
  );
});

test("rejects host origins with paths, credentials, non-HTTP schemes, and normal app routes", () => {
  for (const origin of [
    "https://control.example.com/",
    "https://control.example.com/path",
    "https://user@control.example.com",
    "javascript:alert(1)",
    "null",
    "*",
  ]) {
    assert.equal(
      resolveEmbedHostOrigin({
        pathname: "/embed/channel/T1/C1",
        search: `?hostOrigin=${encodeURIComponent(origin)}`,
      }),
      null,
      origin,
    );
  }
  assert.equal(
    resolveEmbedHostOrigin({
      pathname: "/app/T1/C1",
      search: "?hostOrigin=https%3A%2F%2Fcontrol.example.com",
    }),
    null,
  );
});
