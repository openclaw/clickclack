import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { DEFAULT_HOME_LINK, homeLinkTitle, loadHomeLink, normalizeHomeLink } from "./home-link.ts";

describe("home link", () => {
  it("keeps the ClickClack default when the payload is missing or malformed", () => {
    assert.deepEqual(normalizeHomeLink(undefined), DEFAULT_HOME_LINK);
    assert.deepEqual(normalizeHomeLink("nope"), DEFAULT_HOME_LINK);
    assert.deepEqual(normalizeHomeLink({}), DEFAULT_HOME_LINK);
    assert.equal(homeLinkTitle(DEFAULT_HOME_LINK), "ClickClack home");
  });

  it("accepts an absolute http(s) URL or an absolute path with a short label", () => {
    assert.deepEqual(normalizeHomeLink({ url: "https://mfs.example.com/", label: "MFS" }), {
      url: "https://mfs.example.com/",
      label: "MFS",
    });
    assert.deepEqual(normalizeHomeLink({ url: "/app", label: " Home " }), {
      url: "/app",
      label: "Home",
    });
    assert.equal(homeLinkTitle({ url: "https://mfs.example.com/", label: "MFS" }), "MFS home");
  });

  it("refuses unsafe destinations and oversized labels field by field", () => {
    for (const url of [
      "/\\evil.example.com",
      "/\t/evil.example.com",
      "/\n/evil.example.com",
      "/\r/evil.example.com",
      "https://good.example.com\\@evil.example.com",
      "/portal?q=\x7f",
      "https://user:password@example.com/portal",
      "\n/portal",
    ]) {
      assert.equal(normalizeHomeLink({ url }).url, DEFAULT_HOME_LINK.url, JSON.stringify(url));
    }
    assert.deepEqual(normalizeHomeLink({ url: "javascript:alert(1)", label: "MFS" }), {
      url: DEFAULT_HOME_LINK.url,
      label: "MFS",
    });
    assert.deepEqual(normalizeHomeLink({ url: "//evil.example.com", label: "MFS" }), {
      url: DEFAULT_HOME_LINK.url,
      label: "MFS",
    });
    assert.deepEqual(normalizeHomeLink({ url: "/app", label: "x".repeat(33) }), {
      url: "/app",
      label: DEFAULT_HOME_LINK.label,
    });
  });

  it("falls back to the default when the endpoint fails", async () => {
    assert.deepEqual(
      await loadHomeLink(async () => {
        throw new Error("offline");
      }),
      DEFAULT_HOME_LINK,
    );
    assert.deepEqual(await loadHomeLink(async () => ({ url: "/app", label: "MFS" })), {
      url: "/app",
      label: "MFS",
    });
  });
});
