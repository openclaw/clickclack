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
	eventDeliveryWorkerCount        = 4
	eventDeliveryQueueSize          = 128
	eventDeliveryWorkspaceQueueSize = eventDeliveryQueueSize / eventDeliveryWorkerCount
	eventDeliveryDrainTimeout       = callbackTimeout + 2*time.Second
	eventDeliveryAttemptTimeout     = time.Second
)

var errEventDeliveryQueueFull = errors.New("event delivery queue is full")
var errEventDeliverySubscriptionInactive = errors.New("event delivery subscription is no longer active")

type eventDeliveryJob struct {
	subscriptionID string
	workspaceID    string
	event          store.Event
	payload        []byte
}

type eventDeliveryDispatcher struct {
	server       *Server
	ctx          context.Context
	cancel       context.CancelFunc
	queue        chan eventDeliveryJob
	drainTimeout time.Duration
	startOnce    sync.Once
	closeOnce    sync.Once
	wg           sync.WaitGroup

	mu      sync.Mutex
	pending map[string][]eventDeliveryJob
	ready   []string
	wake    chan struct{}
	closing bool
	aborted bool
}

func newEventDeliveryDispatcher(server *Server) *eventDeliveryDispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &eventDeliveryDispatcher{
		server:       server,
		ctx:          ctx,
		cancel:       cancel,
		queue:        make(chan eventDeliveryJob, eventDeliveryQueueSize),
		drainTimeout: eventDeliveryDrainTimeout,
		pending:      make(map[string][]eventDeliveryJob),
		wake:         make(chan struct{}),
	}
}

func (d *eventDeliveryDispatcher) enqueue(job eventDeliveryJob) bool {
	d.mu.Lock()
	d.initLocked()
	if d.closing {
		d.mu.Unlock()
		return false
	}
	d.startOnce.Do(func() {
		for range eventDeliveryWorkerCount {
			d.wg.Add(1)
			go d.worker()
		}
	})

	workspaceID := eventDeliveryWorkspaceID(job)
	if workspaceID != "" && len(d.pending[workspaceID]) >= eventDeliveryWorkspaceQueueSize {
		d.mu.Unlock()
		return false
	}
	select {
	case d.queue <- job:
	default:
		d.mu.Unlock()
		return false
	}
	if len(d.pending[workspaceID]) == 0 {
		d.ready = append(d.ready, workspaceID)
	}
	d.pending[workspaceID] = append(d.pending[workspaceID], job)
	d.signalLocked()
	d.mu.Unlock()
	return true
}

func (d *eventDeliveryDispatcher) initLocked() {
	if d.ctx == nil || d.cancel == nil {
		d.ctx, d.cancel = context.WithCancel(context.Background())
	}
	if d.queue == nil {
		d.queue = make(chan eventDeliveryJob, eventDeliveryQueueSize)
	}
	if d.drainTimeout <= 0 {
		d.drainTimeout = eventDeliveryDrainTimeout
	}
	if d.pending == nil {
		d.pending = make(map[string][]eventDeliveryJob)
	}
	if d.wake == nil {
		d.wake = make(chan struct{})
	}
}

func (d *eventDeliveryDispatcher) signalLocked() {
	close(d.wake)
	d.wake = make(chan struct{})
}

func eventDeliveryWorkspaceID(job eventDeliveryJob) string {
	if job.event.WorkspaceID != "" {
		return job.event.WorkspaceID
	}
	return job.workspaceID
}

func (d *eventDeliveryDispatcher) nextJob() (eventDeliveryJob, bool) {
	for {
		d.mu.Lock()
		d.initLocked()
		if d.aborted {
			d.mu.Unlock()
			return eventDeliveryJob{}, false
		}
		if len(d.ready) > 0 {
			workspaceID := d.ready[0]
			d.ready = d.ready[1:]
			jobs := d.pending[workspaceID]
			job := jobs[0]
			if len(jobs) == 1 {
				delete(d.pending, workspaceID)
			} else {
				d.pending[workspaceID] = jobs[1:]
				d.ready = append(d.ready, workspaceID)
			}
			<-d.queue
			d.mu.Unlock()
			return job, true
		}
		if d.closing {
			d.mu.Unlock()
			return eventDeliveryJob{}, false
		}
		wake := d.wake
		d.mu.Unlock()
		<-wake
	}
}

func (d *eventDeliveryDispatcher) worker() {
	defer d.wg.Done()
	for {
		job, ok := d.nextJob()
		if !ok {
			return
		}
		d.deliver(job)
	}
}

func (d *eventDeliveryDispatcher) deliver(job eventDeliveryJob) {
	ctx, cancel := context.WithTimeout(d.ctx, callbackTimeout)
	subscription, deliveryErr := d.server.activeEventDeliverySubscription(ctx, job)
	if deliveryErr != nil {
		cancel()
		if d.ctx.Err() != nil {
			return
		}
		d.server.recordEventDeliveryAttempt(context.Background(), job, 0, "", deliveryErr)
		return
	}
	status, responseBody, deliveryErr := d.server.postEventCallback(ctx, subscription, job.event, job.payload)
	cancel()
	if d.ctx.Err() != nil {
		return
	}
	d.server.recordEventDeliveryAttempt(context.Background(), job, status, responseBody, deliveryErr)
}

func (d *eventDeliveryDispatcher) close() {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		d.initLocked()
		d.closing = true
		d.signalLocked()
		drainTimeout := d.drainTimeout
		d.mu.Unlock()

		done := make(chan struct{})
		go func() {
			d.wg.Wait()
			close(done)
		}()

		timer := time.NewTimer(drainTimeout)
		defer timer.Stop()
		select {
		case <-done:
			d.cancel()
		case <-timer.C:
			d.mu.Lock()
			d.aborted = true
			d.pending = make(map[string][]eventDeliveryJob)
			d.ready = nil
			for len(d.queue) > 0 {
				<-d.queue
			}
			d.signalLocked()
			d.mu.Unlock()
			d.cancel()
		}
	})
}

func (s *Server) activeEventDeliverySubscription(ctx context.Context, job eventDeliveryJob) (store.EventSubscription, error) {
	subscriptions, err := s.store.ListEventSubscriptionsForEvent(ctx, job.event)
	if err != nil {
		return store.EventSubscription{}, err
	}
	for _, subscription := range subscriptions {
		if subscription.ID == job.subscriptionID {
			return subscription, nil
		}
	}
	return store.EventSubscription{}, errEventDeliverySubscriptionInactive
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
		job := eventDeliveryJob{
			subscriptionID: subscription.ID,
			workspaceID:    subscription.WorkspaceID,
			event:          event,
			payload:        payload,
		}
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
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), eventDeliveryAttemptTimeout)
	defer cancel()
	_, _ = s.store.CreateEventDeliveryAttempt(recordCtx, store.CreateEventDeliveryAttemptInput{
		SubscriptionID: job.subscriptionID,
		EventID:        job.event.ID,
		WorkspaceID:    job.event.WorkspaceID,
		EventType:      job.event.Type,
		RequestJSON:    string(job.payload),
		ResponseStatus: status,
		ResponseBody:   responseBody,
		Error:          errorText,
	})
}
