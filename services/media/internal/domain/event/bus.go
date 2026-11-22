package event

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Event represents a domain event emitted by an aggregate or use case.
type Event interface {
	EventID() string
	EventName() string
	AggregateID() string
	OccurredAt() time.Time
}

// BaseEvent provides shared metadata for all domain events.
type BaseEvent struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Aggregate  string    `json:"aggregate"`
	OccurredOn time.Time `json:"occurred_on"`
}

func NewBaseEvent(name, aggregateID string) BaseEvent {
	return BaseEvent{
		ID:         uuid.NewString(),
		Name:       name,
		Aggregate:  aggregateID,
		OccurredOn: time.Now().UTC(),
	}
}

func (e BaseEvent) EventID() string       { return e.ID }
func (e BaseEvent) EventName() string     { return e.Name }
func (e BaseEvent) AggregateID() string   { return e.Aggregate }
func (e BaseEvent) OccurredAt() time.Time { return e.OccurredOn }

// EventHandler subscribes to domain events.
type EventHandler interface {
	InterestedIn(eventName string) bool
	Handle(ctx context.Context, evt Event) error
}

// EventBus dispatches domain events to subscribers.
type EventBus interface {
	Publish(ctx context.Context, events ...Event) error
	Subscribe(handler EventHandler)
}

// NoopEventBus discards events (useful for simple tests).
type NoopEventBus struct{}

func (NoopEventBus) Publish(context.Context, ...Event) error { return nil }
func (NoopEventBus) Subscribe(EventHandler)                  {}

// InMemoryEventBus synchronously dispatches domain events to registered handlers.
type InMemoryEventBus struct {
	mu       sync.RWMutex
	handlers []EventHandler
}

func NewInMemoryEventBus() *InMemoryEventBus {
	return &InMemoryEventBus{handlers: make([]EventHandler, 0)}
}

func (b *InMemoryEventBus) Subscribe(handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers = append(b.handlers, handler)
}

func (b *InMemoryEventBus) Publish(ctx context.Context, events ...Event) error {
	b.mu.RLock()
	handlers := make([]EventHandler, len(b.handlers))
	copy(handlers, b.handlers)
	b.mu.RUnlock()

	for _, evt := range events {
		for _, h := range handlers {
			if h.InterestedIn(evt.EventName()) {
				if err := h.Handle(ctx, evt); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
