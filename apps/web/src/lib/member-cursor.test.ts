import assert from "node:assert/strict";
import test from "node:test";
import { nextMemberCursor, type MemberCursorPage } from "./member-cursor.ts";

function page(overrides: Partial<MemberCursorPage> = {}): MemberCursorPage {
  return {
    has_more: false,
    ...overrides,
  };
}

test("nextMemberCursor returns undefined when the directory is exhausted", () => {
  const seen = new Set<string>();
  assert.equal(nextMemberCursor(page({ has_more: false, next_cursor: "c1" }), seen), undefined);
  assert.equal(seen.size, 0);
});

test("nextMemberCursor returns and records a fresh next cursor", () => {
  const seen = new Set<string>();
  assert.equal(nextMemberCursor(page({ has_more: true, next_cursor: "c1" }), seen), "c1");
  assert.equal(nextMemberCursor(page({ has_more: true, next_cursor: "c2" }), seen), "c2");
  assert.deepEqual([...seen], ["c1", "c2"]);
});

test("nextMemberCursor throws when has_more is set without a cursor", () => {
  const seen = new Set<string>();
  assert.throws(() => nextMemberCursor(page({ has_more: true }), seen), {
    message: "Member directory returned an incomplete page",
  });
  assert.throws(() => nextMemberCursor(page({ has_more: true, next_cursor: "" }), seen), {
    message: "Member directory returned an incomplete page",
  });
  assert.equal(seen.size, 0);
});

test("nextMemberCursor throws when the same pagination cursor repeats", () => {
  const seen = new Set<string>(["c1"]);
  assert.throws(() => nextMemberCursor(page({ has_more: true, next_cursor: "c1" }), seen), {
    message: "Member directory repeated a pagination cursor",
  });
  assert.deepEqual([...seen], ["c1"]);
});
