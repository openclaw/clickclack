export type RealtimeQueueEvent = {
  cursor?: string;
};

export type RealtimeQueueOptions<Event extends RealtimeQueueEvent> = {
  onEvent: (event: Event) => void | Promise<void>;
  persistCursor: (cursor: string) => void;
  onError?: (error: unknown) => void;
  onFailure?: () => void;
  isActive?: () => boolean;
  maxPendingEvents?: number;
};

export type RealtimeQueue<Event extends RealtimeQueueEvent> = {
  enqueue(event: Event): void;
  stop(): void;
  whenIdle(): Promise<void>;
};

// The server emits at most 5,000 replay events plus a 32-event live hub burst.
// Keep headroom above that recoverable maximum while bounding sustained live load.
const defaultMaxPendingEvents = 8192;

export function createRealtimeQueue<Event extends RealtimeQueueEvent>(
  options: RealtimeQueueOptions<Event>,
): RealtimeQueue<Event> {
  const isActive = options.isActive ?? (() => true);
  const maxPendingEvents = options.maxPendingEvents ?? defaultMaxPendingEvents;
  const pending: Event[] = [];
  const idleResolvers: Array<() => void> = [];
  let stopped = false;
  let draining = false;

  function active(): boolean {
    if (stopped) return false;
    try {
      return isActive();
    } catch {
      return false;
    }
  }

  function resolveIdle() {
    if (draining || pending.length > 0) return;
    for (const resolve of idleResolvers.splice(0)) resolve();
  }

  function fail(error: unknown) {
    if (!active()) return;
    stopped = true;
    pending.length = 0;
    try {
      options.onError?.(error);
    } catch {
      // Error reporting must not turn a handled event failure into an unhandled rejection.
    }
    try {
      options.onFailure?.();
    } catch {
      // The queue is already stopped; reconnect cleanup is best-effort from here.
    }
    resolveIdle();
  }

  async function drain() {
    if (draining) return;
    draining = true;
    while (active() && pending.length > 0) {
      const event = pending.shift()!;
      try {
        await options.onEvent(event);
        if (!active()) {
          pending.length = 0;
          break;
        }
        if (event.cursor) options.persistCursor(event.cursor);
      } catch (error) {
        fail(error);
        break;
      }
    }
    draining = false;
    resolveIdle();
  }

  return {
    enqueue(event) {
      if (!active()) return;
      if (pending.length >= maxPendingEvents) {
        fail(new Error("Realtime event queue overflowed; reconnecting to recover"));
        return;
      }
      pending.push(event);
      void drain();
    },
    stop() {
      stopped = true;
      pending.length = 0;
      resolveIdle();
    },
    whenIdle() {
      if (!draining && pending.length === 0) return Promise.resolve();
      return new Promise<void>((resolve) => idleResolvers.push(resolve));
    },
  };
}
