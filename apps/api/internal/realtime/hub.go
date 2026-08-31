package realtime

import (
	"sync"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const subscriberBufferSize = 32

type Subscription struct {
	Events <-chan store.Event
	Wake   <-chan struct{}
	Done   <-chan struct{}
}

type subscriber struct {
	events chan store.Event
	wake   chan struct{}
	done   chan struct{}
}

type Hub struct {
	mu   sync.Mutex
	subs map[string]map[*subscriber]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[string]map[*subscriber]struct{}{}}
}

func (h *Hub) Subscribe(workspaceID string) (Subscription, func()) {
	sub := &subscriber{make(chan store.Event, subscriberBufferSize), make(chan struct{}, 1), make(chan struct{})}
	h.mu.Lock()
	if h.subs[workspaceID] == nil {
		h.subs[workspaceID] = map[*subscriber]struct{}{}
	}
	h.subs[workspaceID][sub] = struct{}{}
	h.mu.Unlock()
	return Subscription{sub.events, sub.wake, sub.done}, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.removeSubscriberLocked(workspaceID, sub)
	}
}

func (h *Hub) removeSubscriberLocked(workspaceID string, sub *subscriber) {
	subs := h.subs[workspaceID]
	if _, ok := subs[sub]; !ok {
		return
	}
	delete(subs, sub)
	close(sub.done)
	close(sub.events)
	close(sub.wake)
	if len(subs) == 0 {
		delete(h.subs, workspaceID)
	}
}

func (h *Hub) Publish(event store.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs[event.WorkspaceID] {
		if event.Cursor != "" {
			// Durable receipts only wake the authorized log reader. Even a private
			// receipt can unblock delivery of earlier public events in this workspace.
			select {
			case sub.wake <- struct{}{}:
			default:
			}
			continue
		}
		select {
		case sub.events <- event:
		default:
			h.removeSubscriberLocked(event.WorkspaceID, sub)
		}
	}
}
