<script lang="ts">
  // Lightweight "the agent is working" status line. Distinct from
  // TypingIndicator (human typing) and AgentProgress (per-step tool/thinking
  // detail): this is the single high-level signal that an agent turn is live.
  type Props = {
    active: boolean;
  };

  let { active }: Props = $props();
</script>

<div
  class="typing-indicator agent-responding"
  class:visible={active}
  aria-live="polite"
  aria-atomic="true"
>
  <span class="typing-indicator__dots" aria-hidden="true">
    <i></i><i></i><i></i>
  </span>
  <span class="typing-indicator__label">Agent is responding…</span>
</div>
