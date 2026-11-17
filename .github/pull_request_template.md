<!--
Optional linked context:
Add a visible `Closes #<issue-number>` or `Related: #<issue-number>` line
below this comment.

Required PR title:
type: user-facing description
Use a parenthesized scope only when it adds clarity:
fix(auth): login redirect loops when session cookie is expired

Types: feat, fix, improve, refactor, docs, chore.
For fixes, describe the user-visible symptom and trigger:
fix: new channel messages are missing after reconnecting
Avoid implementation details such as:
fix: reset the event cursor on reconnect

Write for a reader who does not know ClickClack's implementation. Start with
the affected person and workflow, not a filename, helper, or internal acronym.
Use concrete language instead of vague claims like "improves reliability".
-->

<details>
<summary>Maintainer edits</summary>

**MUST:** Keep **Allow edits from maintainers** enabled for this PR so maintainers
can help update the branch when needed.

</details>

## What Problem This Solves

<!--
Describe the concrete user, product, or operational problem.
Use one short, plain-language sentence. For fixes, prefer:
"Fixes: <what goes wrong> when <trigger or condition>."
For other changes, describe the need without inventing a bug.
Name the affected workflow, not the code-level cause.
For example: "Fixes: people miss new channel messages when their connection
drops and reconnects." Examples here are illustrative, not claims of existing bugs.
-->

## User Impact

<!--
"User impact: <what users, operators, or developers can now do or expect>."
Lead with the concrete outcome in plain language, usually one sentence.
For internal-only changes, say there is no user-visible change; do not invent a benefit.
Keep important risks, breaking changes, migrations, and required user actions visible here.
For the reconnect example: "User impact: people can catch up on channel messages
after reconnecting without refreshing the page."
-->

## Why This Change Was Made

<!--
Briefly explain how the change addresses the problem without repeating the impact.
Keep the body short. Leave file lists, internal acronyms, and root-cause walkthroughs
in the diff or optional <details>; include technical detail only when it explains
behavior or a material tradeoff. Do not hide risks or required actions in <details>.
Explain why the approach helps the affected workflow; a list of changed files
or functions is not a substitute.
-->

## Evidence

<!--
Show the most useful proof that this change works. Screenshots, screencasts,
terminal output, focused tests, CI results, live observations, redacted logs,
and artifact links are all useful. Include before/after evidence for visual
changes when it clarifies the result.

Reviewers will inspect the code, tests, and CI. Use this section to make the
validation easy to understand, not to restate the diff.
Summarize what was checked and the result; note meaningful gaps. Link long output
or put it in optional <details>, keeping the useful evidence summary visible.
Lead with the observed outcome, not just a list of commands or test names.
For example, only if verified: "After reconnecting the web client, messages sent
while offline appeared once and in order. The focused reconnect test passed;
desktop reconnect behavior was not checked." Distinguish observed behavior from
what a test covers, and state failures or untested paths rather than implying success.
-->
