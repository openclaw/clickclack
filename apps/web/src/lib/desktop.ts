export type DesktopNotification = {
  body: string;
  route?: string;
  tag?: string;
  title: string;
};

export type ClickClackDesktopBridge = {
  notify(notification: DesktopNotification): Promise<boolean>;
  onNavigate(callback: (route: string) => void): () => void;
  onQuickCompose(callback: () => void): () => void;
  openSettings(): void;
  platform: "darwin" | "linux" | "win32" | string;
  setActiveRoute(route: string): void;
  setUnreadCount(count: number): void;
  signInWithGitHub(): Promise<boolean>;
};

export const desktop: ClickClackDesktopBridge | undefined =
  typeof window === "undefined" ? undefined : window.clickclackDesktop;
