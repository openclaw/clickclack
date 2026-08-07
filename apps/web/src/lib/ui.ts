import { writable, derived } from 'svelte/store';

// ============================================================
// PROJECT LOGOS — Keyboard & UI State Store (Track A)
// ============================================================
// Track B consumes inspectMode, activeMessageId, and
// currentPersona. All stores are writable; Track B may
// also write to activeMessageId for keyboard nav sync.

/** Alt/Option held — diagnostic telemetry mode.
 *  Track B: when true, switch viewport to Diagnostic Telemetry
 *  View (§8.5): text opacity 60%, dashed vector association
 *  lines, token probabilities above key terms. */
export const inspectMode = writable(false);

/** Currently navigated message ID (j/k or arrows).
 *  Track B: highlight or focus the message row. */
export const activeMessageId = writable<string | null>(null);

/** Command palette open state.
 *  Track B: may read to disable conflicting shortcuts. */
export const commandPaletteOpen = writable(false);

/** Active persona set via :persona <name>. */
export type Persona = 'operator' | 'analyst' | 'creative' | 'socratic' | 'archivist';
export const currentPersona = writable<Persona>('operator');

/** Telemetry blade rail open state (thin right column). */
export const telemetryOpen = writable(false);

/** Derived: any chassis-level overlay is open. */
export const chassisOverlayOpen = derived(
  [commandPaletteOpen, telemetryOpen],
  ([$cmd, $telem]) => $cmd || $telem,
);
