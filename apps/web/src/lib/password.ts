// Self-service password change. Two independent facts gate it: the deployment
// has to have password sign-in enabled, which the server advertises in the
// runtime config as an auth method, and the account has to already have a
// password on file, which the server reports on /api/me. An account without one
// is enrolled by an administrator, never here.

import type { User } from "./types";

// Mirrors passwordauth.MinPasswordLength. The server is the authority; this
// only saves a round trip on an obviously short secret.
export const MIN_PASSWORD_LENGTH = 8;

export function canChangePassword(
  user: Pick<User, "password_enrolled">,
  methods: string[],
): boolean {
  return methods.includes("password") && user.password_enrolled === true;
}

// validateNewPassword returns the message to show, or an empty string when the
// form is ready to submit.
export function validateNewPassword(current: string, next: string, confirm: string): string {
  if (!current) return "Enter your current password.";
  if (!next) return "Enter a new password.";
  if (next.length < MIN_PASSWORD_LENGTH) {
    return `New password must be at least ${MIN_PASSWORD_LENGTH} characters.`;
  }
  if (next !== confirm) return "New passwords do not match.";
  return "";
}
