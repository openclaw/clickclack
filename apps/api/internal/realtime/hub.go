package realtime

import (
	"sync"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const subscriberBufferSize = 32

type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan store.Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: map[string]map[chan store.Event]struct{}{}}
}

func (h *Hub) Subscribe(workspaceID string) (<-chan store.Event, func()) {
	ch := make(chan store.Event, subscriberBufferSize)
	h.mu.Lock()
	if h.subs[workspaceID] == nil {
		h.subs[workspaceID] = map[chan store.Event]struct{}{}
	}
	h.subs[workspaceID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.removeSubscriberLocked(workspaceID, ch)
	}
}

func (h *Hub) removeSubscriberLocked(workspaceID string, ch chan store.Event) {
	subs := h.subs[workspaceID]
	if _, ok := subs[ch]; !ok {
		return
	}
	delete(subs, ch)
	close(ch)
	if len(subs) == 0 {
		delete(h.subs, workspaceID)
	}
}

func (h *Hub) Publish(event store.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[event.WorkspaceID]
	for ch := range subs {
		select {
		case ch <- event:
		default:
			h.removeSubscriberLocked(event.WorkspaceID, ch)
		}
	}
}

func (h *Hub) PublishMany(events []store.Event) {
	for _, event := range events {
		h.Publish(event)
	}
}
