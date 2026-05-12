package events

import "sync"

// EventEmitter is the interface for emitting events (used by nodes to avoid circular deps).
type EventEmitter interface {
	Publish(event Event)
}

// EventBus is a pub/sub event bus.
// - Publish sends events to all matching subscribers (non-blocking; drops if buffer full).
// - Subscribe returns a channel to receive events of the specified types.
// - Unsubscribe removes a subscriber channel.
// - History returns the last N events (for new WebSocket connections).
// - Ring buffer holds last 500 events.
type EventBus struct {
	publish chan Event
	subs    map[chan Event][]EventType
	mu      sync.RWMutex
	history []Event
	histMax int
	stopCh  chan struct{}
}

// NewEventBus creates and starts an EventBus.
func NewEventBus() *EventBus {
	b := &EventBus{
		publish: make(chan Event, 1000),
		subs:    make(map[chan Event][]EventType),
		histMax: 500,
		stopCh:  make(chan struct{}),
	}
	go b.run()
	return b
}

// Publish emits an event to the bus (non-blocking, drops on full buffer).
func (b *EventBus) Publish(event Event) {
	select {
	case b.publish <- event:
	default:
		// buffer full: drop
	}
}

// Subscribe returns a buffered channel that receives events matching types.
// Pass []EventType{EventAll} to receive all events.
func (b *EventBus) Subscribe(types []EventType) chan Event {
	ch := make(chan Event, 256)
	b.mu.Lock()
	b.subs[ch] = types
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes the subscriber and closes its channel.
func (b *EventBus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
	close(ch)
}

// History returns a copy of stored events (up to histMax, newest first).
func (b *EventBus) History() []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cp := make([]Event, len(b.history))
	copy(cp, b.history)
	return cp
}

// Stop halts the event bus dispatcher goroutine.
func (b *EventBus) Stop() {
	close(b.stopCh)
}

func (b *EventBus) run() {
	for {
		select {
		case <-b.stopCh:
			return
		case event := <-b.publish:
			b.dispatch(event)
		}
	}
}

func (b *EventBus) dispatch(event Event) {
	b.mu.Lock()
	// Add to history ring buffer
	b.history = append(b.history, event)
	if len(b.history) > b.histMax {
		b.history = b.history[len(b.history)-b.histMax:]
	}
	// Copy subscriber map to avoid holding lock during sends
	type sub struct {
		ch    chan Event
		types []EventType
	}
	subs := make([]sub, 0, len(b.subs))
	for ch, types := range b.subs {
		subs = append(subs, sub{ch, types})
	}
	b.mu.Unlock()

	// Deliver to matching subscribers
	for _, s := range subs {
		if matchesType(s.types, event.Type) {
			select {
			case s.ch <- event:
			default:
				// subscriber buffer full: drop for this subscriber
			}
		}
	}
}

func matchesType(types []EventType, t EventType) bool {
	for _, ty := range types {
		if ty == EventAll || ty == t {
			return true
		}
	}
	return false
}
