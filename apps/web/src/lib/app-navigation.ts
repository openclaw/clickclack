type Navigate = (
  path: string,
  options: { replaceState: boolean; noScroll: boolean; keepFocus: boolean },
) => Promise<void>;

export function createAppNavigator(currentPath: () => string, navigate: Navigate) {
  let pending = 0;
  return async (path: string, replaceState = false) => {
    // The visible URL may still belong to the route a pending navigation leaves.
    if (currentPath() === path && pending === 0) return;
    pending += 1;
    try {
      await navigate(path, { replaceState, noScroll: true, keepFocus: true });
    } finally {
      pending -= 1;
    }
  };
}
