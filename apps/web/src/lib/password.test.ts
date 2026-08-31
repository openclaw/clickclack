import test from "node:test";
import assert from "node:assert/strict";
import { MIN_PASSWORD_LENGTH, canChangePassword, validateNewPassword } from "./password.ts";

test("canChangePassword needs both the deployment flag and an enrolled account", () => {
  assert.equal(canChangePassword({ password_enrolled: true }, ["password"]), true);
  assert.equal(canChangePassword({ password_enrolled: true }, ["github"]), false);
  assert.equal(canChangePassword({ password_enrolled: false }, ["password"]), false);
  assert.equal(canChangePassword({}, ["password"]), false);
  assert.equal(canChangePassword({ password_enrolled: true }, []), false);
});

test("validateNewPassword reports the first unmet requirement", () => {
  assert.equal(validateNewPassword("", "longenough", "longenough"), "Enter your current password.");
  assert.equal(validateNewPassword("current", "", ""), "Enter a new password.");
  assert.equal(
    validateNewPassword("current", "short", "short"),
    `New password must be at least ${MIN_PASSWORD_LENGTH} characters.`,
  );
  assert.equal(
    validateNewPassword("current", "longenough", "longenougi"),
    "New passwords do not match.",
  );
});

test("validateNewPassword accepts a confirmed password at the minimum length", () => {
  const atMinimum = "x".repeat(MIN_PASSWORD_LENGTH);
  assert.equal(validateNewPassword("current", atMinimum, atMinimum), "");
});
