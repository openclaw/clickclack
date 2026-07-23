package realtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

func TestHubSubscribePublishAndUnsubscribe(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	events, unsubscribe := hub.Subscribe("wsp_1")
	event := store.Event{ID: "evt_1", WorkspaceID: "wsp_1", Type: "message.created"}
	hub.Publish(event)

	select {
	case got := <-events:
		if got.ID != event.ID {
			t.Fatalf("expected %s, got %s", event.ID, got.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	hub.PublishMany([]store.Event{{ID: "evt_2", WorkspaceID: "wsp_other"}})
	select {
	case got := <-events:
		t.Fatalf("unexpected event for other workspace: %#v", got)
	case <-time.After(10 * time.Millisecond):
	}

	unsubscribe()
	unsubscribe()
	_, ok := <-events
	if ok {
		t.Fatal("expected subscription channel to close")
	}
}

func TestHubOverflowClosesOnlySlowSubscriber(t *testing.T) {
	t.Parallel()
	hub := NewHub()
	slow, unsubscribeSlow := hub.Subscribe("wsp_1")
	healthy, unsubscribeHealthy := hub.Subscribe("wsp_1")
	isolated, unsubscribeIsolated := hub.Subscribe("wsp_2")
	defer unsubscribeHealthy()
	defer unsubscribeIsolated()

	for i := 0; i < subscriberBufferSize; i++ {
		event := store.Event{ID: fmt.Sprintf("evt_%02d", i), WorkspaceID: "wsp_1"}
		hub.Publish(event)
		if got := receiveEvent(t, healthy); got.ID != event.ID {
			t.Fatalf("healthy subscriber got %q, want %q", got.ID, event.ID)
		}
	}

	overflow := store.Event{ID: "evt_overflow", WorkspaceID: "wsp_1"}
	hub.Publish(overflow)
	if got := receiveEvent(t, healthy); got.ID != overflow.ID {
		t.Fatalf("healthy subscriber got %q, want %q", got.ID, overflow.ID)
	}

	for i := 0; i < subscriberBufferSize; i++ {
		got, ok := <-slow
		if !ok {
			t.Fatalf("slow subscriber closed before buffered event %d", i)
		}
		want := fmt.Sprintf("evt_%02d", i)
		if got.ID != want {
			t.Fatalf("slow subscriber got %q, want %q", got.ID, want)
		}
	}
	if _, ok := <-slow; ok {
		t.Fatal("expected overflowed subscriber channel to close")
	}

	unsubscribeSlow()
	unsubscribeSlow()

	afterOverflow := store.Event{ID: "evt_after_overflow", WorkspaceID: "wsp_1"}
	hub.Publish(afterOverflow)
	if got := receiveEvent(t, healthy); got.ID != afterOverflow.ID {
		t.Fatalf("healthy subscriber got %q, want %q", got.ID, afterOverflow.ID)
	}
	select {
	case event := <-isolated:
		t.Fatalf("isolated subscriber received other workspace event: %#v", event)
	default:
	}

	isolatedEvent := store.Event{ID: "evt_isolated", WorkspaceID: "wsp_2"}
	hub.Publish(isolatedEvent)
	if got := receiveEvent(t, isolated); got.ID != isolatedEvent.ID {
		t.Fatalf("isolated subscriber got %q, want %q", got.ID, isolatedEvent.ID)
	}
}

func receiveEvent(t *testing.T, events <-chan store.Event) store.Event {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("subscription closed while waiting for event")
		}
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return store.Event{}
	}
}
