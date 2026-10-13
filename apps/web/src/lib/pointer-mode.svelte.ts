// Tablets can report a coarse primary pointer even while a trackpad is in use.
export const pointerMode = $state({ mouse: false });

const ATTRIBUTE = "data-pointer-mode";

export function startPointerModeTracking(): () => void {
  const finePointer = window.matchMedia("(any-pointer: fine)");
  const apply = (mouse: boolean) => {
    if (pointerMode.mouse === mouse) return;
    pointerMode.mouse = mouse;
    if (mouse) document.documentElement.setAttribute(ATTRIBUTE, "mouse");
    else document.documentElement.removeAttribute(ATTRIBUTE);
  };
  const onPointer = (event: PointerEvent) => {
    if (event.pointerType === "mouse") apply(finePointer.matches);
    else if (event.pointerType === "touch") apply(false);
  };
  window.addEventListener("pointermove", onPointer, { capture: true, passive: true });
  window.addEventListener("pointerdown", onPointer, { capture: true, passive: true });
  return () => {
    window.removeEventListener("pointermove", onPointer, { capture: true });
    window.removeEventListener("pointerdown", onPointer, { capture: true });
    apply(false);
  };
}
