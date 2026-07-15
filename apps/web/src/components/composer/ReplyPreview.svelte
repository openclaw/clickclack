<script lang="ts">
  import { quoteSnippet } from "../../lib/chat/messages";
  import { userDisplayLabel } from "../../lib/chat/people";
  import type { Message } from "../../lib/types";

  type Props = {
    target: Message;
    onClear: () => void;
  };

  let { target, onClear }: Props = $props();
</script>

<div class="quote-preview" aria-label="Replying to message">
  <span class="quote-bar" aria-hidden="true"></span>
  <span class="quote-preview-body">
    <span class="quote-preview-label">Replying to <strong>{userDisplayLabel(target.author)}</strong></span>
    <span class="quote-preview-snippet">{quoteSnippet(target.body)}</span>
  </span>
  <button type="button" class="quote-preview-clear" aria-label="Cancel reply" onclick={onClear}>×</button>
</div>
