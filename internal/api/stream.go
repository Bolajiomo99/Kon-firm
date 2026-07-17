package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Bolajiomo99/Kon-firm/internal/auth"
	"github.com/Bolajiomo99/Kon-firm/internal/events"
)

// heartbeatInterval keeps the connection alive.
//
// Idle proxies and load balancers close connections that go quiet — Render's
// included. A comment line every 25 seconds costs nothing and stops the stream
// dying silently, which would otherwise look exactly like "the dashboard
// stopped updating".
const heartbeatInterval = 25 * time.Second

// handleStream is a Server-Sent Events endpoint for live UI updates.
//
// SSE rather than WebSocket: this traffic only ever flows server-to-browser.
// SSE is a plain HTTP response, so it needs no upgrade handshake, no extra
// library, and no special proxy handling — and EventSource reconnects on its
// own when a connection drops, which a WebSocket does not.
//
// Scope is decided here, from the session, never from a query parameter:
//   - admins subscribe to everything
//   - anyone else may watch a single order by reference
//
// Letting the client name its own topic would let any visitor stream every
// order in the shop.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	user := userFrom(r.Context())
	orderRef := r.URL.Query().Get("order")

	var topic events.Topic
	var ref string

	switch {
	case user != nil && user.Role == auth.RoleAdmin:
		topic = events.TopicAdmin
	case orderRef != "":
		// Order references are unguessable (timestamp plus 8 random bytes), so
		// holding one is treated as proof you placed it — the same assumption
		// the receipt page already makes.
		topic = events.TopicOrder
		ref = orderRef
	default:
		writeError(w, http.StatusForbidden, "nothing to stream")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Render sits behind a proxy that will buffer a streaming response into
	// uselessness without this.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, unsubscribe := s.events.Subscribe(topic, ref)
	defer unsubscribe()

	// Tell the client we are live, so it can show a connected state rather
	// than waiting for the first real event to prove the stream works.
	fmt.Fprintf(w, "event: ready\ndata: {\"topic\":\"%s\"}\n\n", topic)
	flusher.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client navigated away or the connection dropped.
			return

		case ev, open := <-ch:
			if !open {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				s.log.Error("stream: marshal event", "err", err)
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
			flusher.Flush()

		case <-ticker.C:
			// A comment line: valid SSE, ignored by EventSource, keeps the
			// connection from being reaped as idle.
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
