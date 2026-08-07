import { d as derived, w as writable } from "./index2.js";
const inspectMode = writable(false);
const commandPaletteOpen = writable(false);
const telemetryOpen = writable(false);
const semanticPaneOpen = writable(false);
derived(
  [commandPaletteOpen, telemetryOpen],
  ([$cmd, $telem]) => $cmd || $telem
);
export {
  inspectMode as i,
  semanticPaneOpen as s,
  telemetryOpen as t
};
