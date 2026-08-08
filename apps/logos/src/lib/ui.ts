// PROJECT LOGOS — shared UI state (app shell)
import { writable, derived } from "svelte/store";

/** Alt/Option held — diagnostic telemetry view (spec §8.5). */
export const inspectMode = writable(false);

/** Currently focused message id (j/k navigation). */
export const activeMessageId = writable<string | null>(null);

/** Command palette open state. */
export const commandPaletteOpen = writable(false);

/** Active persona (default operator). */
export type Persona = "operator" | "analyst" | "creative" | "socratic" | "archivist";
export const currentPersona = writable<Persona>("operator");

/** Right telemetry rail open. */
export const telemetryOpen = writable(false);

/** Semantic threads pane open. */
export const semanticPaneOpen = writable(false);

/** Operator notifications / inline feedback. */
export const operatorNotice = writable<string | null>(null);

/** Any chassis overlay open. */
export const chassisOverlayOpen = derived(
  [commandPaletteOpen, telemetryOpen],
  ([$cmd, $telem]) => $cmd || $telem,
);
