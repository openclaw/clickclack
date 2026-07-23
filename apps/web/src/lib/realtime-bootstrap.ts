export const realtimeResyncRequiredCode = 4001;

export function requiresRealtimeResync(code: number): boolean {
  return code === realtimeResyncRequiredCode;
}

export type RealtimeCursorBootstrapOptions = {
  readCursor: () => string | null;
  fetchTailCursor: () => Promise<string>;
  isActive: () => boolean;
};

export type RealtimeCursorBootstrapResult =
  | { active: false }
  | { active: true; cursor: string; persistAfterOpen: boolean };

export async function prepareRealtimeCursor(
  options: RealtimeCursorBootstrapOptions,
): Promise<RealtimeCursorBootstrapResult> {
  const storedCursor = options.readCursor();
  if (!options.isActive()) return { active: false };
  if (storedCursor !== null && storedCursor !== "") {
    return { active: true, cursor: storedCursor, persistAfterOpen: false };
  }

  const tailCursor = await options.fetchTailCursor();
  if (!options.isActive()) return { active: false };
  return { active: true, cursor: tailCursor, persistAfterOpen: true };
}
