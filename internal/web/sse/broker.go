// Package sse implements a simple SSE fan-out broker for live guild updates.
package sse

import (
	"sync"
)

// Event is a single SSE event sent to subscribers.
type Event struct {
	ID   string
	Type string // "message", "member", "sync_status"
	Data string // HTML fragment or JSON
}

// Broker fans out events to per-guild subscriber channels.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan Event]struct{}
}

// NewBroker creates a ready-to-use Broker.
func NewBroker() *Broker {
	return &Broker{
		subscribers: make(map[string]map[chan Event]struct{}),
	}
}

// Subscribe registers a buffered channel for the given guildID.
func (b *Broker) Subscribe(guildID string) chan Event {
	ch := make(chan Event, 16)
	b.mu.Lock()
	if b.subscribers[guildID] == nil {
		b.subscribers[guildID] = make(map[chan Event]struct{})
	}
	b.subscribers[guildID][ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a channel from the guild's subscriber set and closes it.
func (b *Broker) Unsubscribe(guildID string, ch chan Event) {
	b.mu.Lock()
	if subs, ok := b.subscribers[guildID]; ok {
		delete(subs, ch)
		if len(subs) == 0 {
			delete(b.subscribers, guildID)
		}
	}
	b.mu.Unlock()
	// Drain and close to unblock any pending reads.
	for {
		select {
		case <-ch:
		default:
			close(ch)
			return
		}
	}
}

// Publish sends an event to all subscribers of guildID.
// Drops the event for slow consumers rather than blocking.
// Holds the read lock during fan-out to prevent Unsubscribe from closing
// a channel while we are writing to it.
func (b *Broker) Publish(guildID string, event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers[guildID] {
		select {
		case ch <- event:
		default:
			// slow consumer; drop
		}
	}
}
