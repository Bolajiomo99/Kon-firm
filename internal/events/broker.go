// Package events is an in-process pub/sub hub for live UI updates.
//
// Delivery is best-effort and deliberately so. These events make a dashboard
// tick over without a refresh; they are not the source of truth. Anything that
// matters is already committed to Postgres before an event is published, and
// every page can rebuild its whole state from the API. A dropped event costs a
// few seconds of staleness, never correctness.
//
// This is why the broker lives in memory rather than in Redis: with more than
// one instance, a client connected to instance A would miss events published
// on instance B — and the page would still be right, just later.
package events

import (
	"sync"
	"time"
)

// Topic scopes a subscription.
type Topic string

const (
	// TopicAdmin carries everything a dashboard cares about.
	TopicAdmin Topic = "admin"
	// TopicOrder is per-order, so a customer's receipt page only ever sees
	// their own order and never another customer's activity.
	TopicOrder Topic = "order"
)

// Event is what a subscriber receives.
type Event struct {
	Type string `json:"type"`
	// Ref scopes the event: an order reference for TopicOrder.
	Ref  string `json:"ref,omitempty"`
	Data any    `json:"data,omitempty"`
	At   int64  `json:"at"`
}

// Event types.
const (
	TypeOrderCreated = "order.created"
	TypeOrderPaid    = "order.paid"
	TypeOrderFailed  = "order.failed"
	TypeRefundIssued = "refund.issued"
	TypeRefundDone   = "refund.completed"
	TypeStockChanged = "stock.changed"
	TypePing         = "ping"
)

type subscriber struct {
	ch    chan Event
	topic Topic
	ref   string // for TopicOrder; empty means all
}

// Broker fans events out to connected clients.
type Broker struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[*subscriber]struct{})}
}

// Subscribe returns a channel of events and a function to release it.
// The caller MUST call the returned func, or the subscriber leaks for the
// lifetime of the process.
func (b *Broker) Subscribe(topic Topic, ref string) (<-chan Event, func()) {
	// Buffered: a slow client must not block the publisher. If a subscriber
	// falls this far behind, its events are dropped rather than stalling the
	// request that published them — a webhook must never wait on a browser.
	s := &subscriber{ch: make(chan Event, 16), topic: topic, ref: ref}

	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()

	return s.ch, func() {
		b.mu.Lock()
		if _, ok := b.subs[s]; ok {
			delete(b.subs, s)
			close(s.ch)
		}
		b.mu.Unlock()
	}
}

// Publish delivers an event to matching subscribers. It never blocks.
func (b *Broker) Publish(topic Topic, ev Event) {
	ev.At = time.Now().UnixMilli()

	b.mu.RLock()
	defer b.mu.RUnlock()

	for s := range b.subs {
		if s.topic != topic {
			continue
		}
		// An order subscriber only hears about its own order.
		if s.topic == TopicOrder && s.ref != ev.Ref {
			continue
		}
		select {
		case s.ch <- ev:
		default:
			// Subscriber is not keeping up. Drop rather than block: the page
			// will still be correct on its next fetch.
		}
	}
}

// PublishOrder notifies both the order's own watcher and the admin dashboard,
// since every order event is also a dashboard event.
func (b *Broker) PublishOrder(evType, orderRef string, data any) {
	ev := Event{Type: evType, Ref: orderRef, Data: data}
	b.Publish(TopicOrder, ev)
	b.Publish(TopicAdmin, ev)
}

// Subscribers reports the current connection count, for health output.
func (b *Broker) Subscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
