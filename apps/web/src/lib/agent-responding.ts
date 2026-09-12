import type { User, WorkspaceBotCommand } from "./types";

export type RespondingAgentTurn = {
  turnId: string;
  userId: string;
  lines: Array<{ finalized: boolean }>;
};

export function agentNameFor(
  userId: string,
  botCommands: WorkspaceBotCommand[],
  lookupUser: (userId: string) => User | undefined,
): string {
  const bot = botCommands.find((command) => command.bot.id === userId)?.bot;
  const user = bot ?? lookupUser(userId);
  return user?.display_name?.trim() || (user?.handle ? `@${user.handle}` : "");
}

export function respondingAgentNames(
  turns: RespondingAgentTurn[],
  botCommands: WorkspaceBotCommand[],
  lookupUser: (userId: string) => User | undefined,
): string[] {
  const seenUserIDs = new Set<string>();
  const names: string[] = [];
  for (const turn of turns) {
    if (!turn.lines.some((line) => !line.finalized)) continue;
    // Empty IDs are unresolved senders. They should not manufacture a named
    // agent, and distinct unresolved turns must not suppress each other.
    if (turn.userId && seenUserIDs.has(turn.userId)) continue;
    if (turn.userId) seenUserIDs.add(turn.userId);
    const name = agentNameFor(turn.userId, botCommands, lookupUser);
    if (name) names.push(name);
  }
  return names;
}
