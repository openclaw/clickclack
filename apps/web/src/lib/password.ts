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

export function validateNewPassword(current: string, next: string, confirm: string): string {
  if (!current) return "Enter your current password.";
  if (!next) return "Enter a new password.";
  if ([...next].length < MIN_PASSWORD_LENGTH) {
    return `New password must be at least ${MIN_PASSWORD_LENGTH} characters.`;
  }
  if (next !== confirm) return "New passwords do not match.";
  return "";
}
