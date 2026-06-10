<script lang="ts" module>
  // Progress lines decay after the turn goes quiet. Generous TTL: turns can
  // legitimately pause for many seconds between tool calls.
  export const AGENT_PROGRESS_TTL_MS = 45_000;

  export type AgentProgressLineView = {
    id: string;
    kind: string;
    text: string;
    toolName?: string;
    status?: string;
    finalized: boolean;
  };

  export type AgentProgressTurn = {
    turnId: string;
    userId: string;
    lines: AgentProgressLineView[];
    expiresAt: number;
  };
</script>

<script lang="ts">
  type Props = {
    turns: AgentProgressTurn[];
  };

  let { turns }: Props = $props();

  function lineLabel(line: AgentProgressLineView): string {
    if (line.kind === "tool" && line.toolName) {
      if (!line.text || line.text === line.toolName) return line.toolName;
      return `${line.toolName}: ${line.text}`;
    }
    return line.text;
  }

  function lineIcon(kind: string): string {
    switch (kind) {
      case "tool":
        return "⚙";
      case "thinking":
      case "commentary":
        return "✦";
      case "plan":
        return "☰";
      case "patch":
        return "±";
      case "command_output":
        return "›";
      case "error":
        return "✕";
      default:
        return "·";
    }
  }
</script>

{#if turns.length > 0}
  <div class="agent-progress" aria-live="polite">
    {#each turns as turn (turn.turnId)}
      <div class="agent-progress__turn">
        {#each turn.lines as line (line.id)}
          <div
            class="agent-progress__line"
            class:agent-progress__line--done={line.finalized}
            data-kind={line.kind}
          >
            <span class="agent-progress__icon" aria-hidden="true">{lineIcon(line.kind)}</span>
            <span class="agent-progress__text">{lineLabel(line)}</span>
          </div>
        {/each}
      </div>
    {/each}
  </div>
{/if}

<style>
  .agent-progress {
    margin: 0 16px 2px;
    padding: 6px 10px;
    border-left: 2px solid var(--agent-progress-accent, #c9a227);
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 132px;
    overflow: hidden;
    justify-content: flex-end;
  }

  .agent-progress__turn {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  .agent-progress__line {
    display: flex;
    align-items: baseline;
    gap: 6px;
    font-size: 12px;
    line-height: 1.45;
    color: var(--agent-progress-fg, #b8a04a);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .agent-progress__line--done {
    opacity: 0.55;
  }

  .agent-progress__icon {
    flex: none;
    width: 14px;
    text-align: center;
    opacity: 0.8;
  }

  .agent-progress__text {
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
