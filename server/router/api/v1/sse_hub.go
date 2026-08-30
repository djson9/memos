package v1

import (
	"encoding/json"
	"sync"
)

const (
	sseClientEventBufferSize = 32
	memoChangedSSEFrame      = "data: {\"type\":\"memo.changed\"}\n\n"
	spaceChangedSSEFrame     = "data: {\"type\":\"space.changed\"}\n\n"
)

// SSEClient represents a single SSE connection.
type SSEClient struct {
	events chan []byte
	done   chan struct{}
	userID int32
}

// SSEHub manages SSE client connections and broadcasts events.
// It is safe for concurrent use.
type SSEHub struct {
	mu      sync.RWMutex
	clients map[*SSEClient]struct{}
	closed  bool
}

// NewSSEHub creates a new SSE hub.
func NewSSEHub() *SSEHub {
	return &SSEHub{
		clients: make(map[*SSEClient]struct{}),
	}
}

// Subscribe registers a new client and returns it.
// The caller must call Unsubscribe when done.
func (h *SSEHub) Subscribe(userIDs ...int32) *SSEClient {
	var userID int32
	if len(userIDs) > 0 {
		userID = userIDs[0]
	}
	c := &SSEClient{
		// Buffer a few events so a slow client doesn't block broadcasting.
		events: make(chan []byte, sseClientEventBufferSize),
		done:   make(chan struct{}),
		userID: userID,
	}
	h.mu.Lock()
	if h.closed {
		close(c.done)
		close(c.events)
	} else {
		h.clients[c] = struct{}{}
	}
	h.mu.Unlock()
	return c
}

// Unsubscribe removes a client and closes its channels.
func (h *SSEHub) Unsubscribe(c *SSEClient) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.done)
		close(c.events)
	}
	h.mu.Unlock()
}

// Close disconnects all subscribed SSE clients.
func (h *SSEHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for c := range h.clients {
		delete(h.clients, c)
		close(c.done)
		close(c.events)
	}
}

// publishMemoChanged tells connected clients to refresh memo-backed caches.
// The event deliberately carries no subject or authorization-sensitive data.
// Slow clients with a full buffer are disconnected so they can reconnect and
// resynchronize instead of silently missing an event.
func (h *SSEHub) publishMemoChanged() {
	h.publish([]byte(memoChangedSSEFrame))
}

// publishSpaceChanged tells connected clients to refresh Space-backed caches
// and caches whose visibility or presentation depends on Space state.
// Like memo.changed, the event carries no authorization-sensitive data.
func (h *SSEHub) publishSpaceChanged() {
	h.publish([]byte(spaceChangedSSEFrame))
}

// publishAttachmentProgress sends upload state only to the user who owns the
// upload. Unlike cache invalidations, filenames and upload identifiers are not
// safe to broadcast to every authenticated client.
func (h *SSEHub) publishAttachmentProgress(userID int32, name, filename, stage string, progress int) {
	payload, err := json.Marshal(struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Filename string `json:"filename"`
		Stage    string `json:"stage"`
		Progress int    `json:"progress"`
	}{
		Type:     "attachment.progress",
		Name:     name,
		Filename: filename,
		Stage:    stage,
		Progress: progress,
	})
	if err != nil {
		return
	}
	frame := append([]byte("data: "), payload...)
	frame = append(frame, '\n', '\n')
	h.publishToUser(userID, frame)
}

func (h *SSEHub) publish(frame []byte) {
	var slowClients []*SSEClient
	h.mu.RLock()
	for c := range h.clients {
		select {
		case c.events <- frame:
		default:
			slowClients = append(slowClients, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range slowClients {
		h.Unsubscribe(c)
	}
}

func (h *SSEHub) publishToUser(userID int32, frame []byte) {
	var slowClients []*SSEClient
	h.mu.RLock()
	for c := range h.clients {
		if c.userID != userID {
			continue
		}
		select {
		case c.events <- frame:
		default:
			slowClients = append(slowClients, c)
		}
	}
	h.mu.RUnlock()

	for _, c := range slowClients {
		h.Unsubscribe(c)
	}
}
