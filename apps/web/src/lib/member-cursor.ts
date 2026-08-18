export type MemberCursorPage = {
  has_more: boolean;
  next_cursor?: string;
};

export function nextMemberCursor(
  page: MemberCursorPage,
  seenCursors: Set<string>,
): string | undefined {
  const cursor = page.has_more ? page.next_cursor : undefined;
  if (page.has_more && !cursor) {
    throw new Error("Member directory returned an incomplete page");
  }
  if (cursor && seenCursors.has(cursor)) {
    throw new Error("Member directory repeated a pagination cursor");
  }
  if (cursor) seenCursors.add(cursor);
  return cursor;
}
