package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/openclaw/clickclack/apps/api/internal/store"
)

const (
	eventDeliveryWorkerCount = 4
	eventDeliveryQueueSize   = 128
)

var errEventDeliveryQueueFull = errors.New("event delivery queue is full")

type eventDeliveryJob struct {
	subscription store.EventSubscription
	event        store.Event
	payload      []byte
}

type eventDeliveryDispatcher struct {
	server    *Server
	ctx       context.Context
	cancel    context.CancelFunc
	queue     chan eventDeliveryJob
	startOnce sync.Once
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func newEventDeliveryDispatcher(server *Server) *eventDeliveryDispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &eventDeliveryDispatcher{
		server: server,
		ctx:    ctx,
		cancel: cancel,
		queue:  make(chan eventDeliveryJob, eventDeliveryQueueSize),
	}
}

func (d *eventDeliveryDispatcher) enqueue(job eventDeliveryJob) bool {
	d.startOnce.Do(func() {
		for range eventDeliveryWorkerCount {
			d.wg.Add(1)
			go d.worker()
		}
	})
	select {
	case <-d.ctx.Done():
		return false
	case d.queue <- job:
		return true
	default:
		return false
	}
}

func (d *eventDeliveryDispatcher) worker() {
	defer d.wg.Done()
	for {
		select {
		case <-d.ctx.Done():
			return
		case job := <-d.queue:
			d.deliver(job)
		}
	}
}

func (d *eventDeliveryDispatcher) deliver(job eventDeliveryJob) {
	ctx, cancel := context.WithTimeout(d.ctx, callbackTimeout)
	status, responseBody, deliveryErr := d.server.postEventCallback(ctx, job.subscription, job.event, job.payload)
	cancel()
	d.server.recordEventDeliveryAttempt(d.ctx, job, status, responseBody, deliveryErr)
}

func (d *eventDeliveryDispatcher) close() {
	d.closeOnce.Do(func() {
		d.cancel()
		d.wg.Wait()
	})
}

func (s *Server) enqueueEventDeliveries(ctx context.Context, event store.Event) {
	subscriptions, err := s.store.ListEventSubscriptionsForEvent(ctx, event)
	if err != nil {
		return
	}
	for _, subscription := range subscriptions {
		payload, err := json.Marshal(map[string]any{
			"subscription_id": subscription.ID,
			"event":           event,
		})
		if err != nil {
			continue
		}
		job := eventDeliveryJob{subscription: subscription, event: event, payload: payload}
		if !s.eventDeliveries.enqueue(job) {
			s.recordEventDeliveryAttempt(ctx, job, 0, "", errEventDeliveryQueueFull)
		}
	}
}

func (s *Server) recordEventDeliveryAttempt(ctx context.Context, job eventDeliveryJob, status int, responseBody string, deliveryErr error) {
	errorText := ""
	if deliveryErr != nil {
		errorText = deliveryErr.Error()
	}
	recordCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, _ = s.store.CreateEventDeliveryAttempt(recordCtx, store.CreateEventDeliveryAttemptInput{
		SubscriptionID: job.subscription.ID,
		EventID:        job.event.ID,
		WorkspaceID:    job.event.WorkspaceID,
		EventType:      job.event.Type,
		RequestJSON:    string(job.payload),
		ResponseStatus: status,
		ResponseBody:   responseBody,
		Error:          errorText,
	})
}
