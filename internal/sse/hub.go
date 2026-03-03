// Package sse implements a Server-Sent Events hub for broadcasting live quota
// statistics to connected clients.
package sse

import (
	"sync"
)

const (
	// clientChannelBuffer is the number of messages buffered per client channel.
	// If a client is too slow to consume messages, the channel fills and the
	// Broadcast call drops the message via the select/default path rather than blocking.
	clientChannelBuffer = 16
)

// Hub manages the set of active SSE client channels and broadcasts messages
// to all of them. It is safe for concurrent use.
type Hub struct {
	mu      sync.Mutex
	clients map[string]chan []byte
}

// NewHub constructs an empty Hub ready to accept client registrations.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]chan []byte),
	}
}

// Register adds a new SSE client identified by clientID and returns a receive-only
// channel that the SSE handler goroutine should read from. The channel is buffered
// to prevent slow clients from stalling broadcasts.
func (h *Hub) Register(clientID string) <-chan []byte {
	ch := make(chan []byte, clientChannelBuffer)
	h.mu.Lock()
	h.clients[clientID] = ch
	h.mu.Unlock()
	return ch
}

// Deregister removes the client identified by clientID from the Hub and closes
// its channel. The SSE handler goroutine must call Deregister when the client
// disconnects (detected via request context cancellation).
func (h *Hub) Deregister(clientID string) {
	h.mu.Lock()
	ch, ok := h.clients[clientID]
	if ok {
		delete(h.clients, clientID)
		close(ch)
	}
	h.mu.Unlock()
}

// Broadcast sends data to every registered client channel. Slow clients whose
// channels are full are skipped via the select/default path — Broadcast never
// blocks, and dropped messages are not retried.
func (h *Hub) Broadcast(data []byte) {
	// Copy the client map under lock to minimise lock hold time during sends.
	h.mu.Lock()
	snapshot := make(map[string]chan []byte, len(h.clients))
	for id, ch := range h.clients {
		snapshot[id] = ch
	}
	h.mu.Unlock()

	for _, ch := range snapshot {
		select {
		case ch <- data:
		default:
			// Client channel is full; drop the message and continue.
		}
	}
}
