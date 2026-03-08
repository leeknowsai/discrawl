package sse

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// ServeHTTP handles a single SSE connection for a guild.
func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	guildID := chi.URLParam(r, "guildID")
	if guildID == "" {
		http.Error(w, "missing guildID", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ch := b.Subscribe(guildID)
	defer b.Unsubscribe(guildID, ch)

	// Send initial heartbeat so the client knows the connection is live.
	_, _ = fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.ID != "" {
				_, _ = fmt.Fprintf(w, "id: %s\n", event.ID)
			}
			if event.Type != "" {
				_, _ = fmt.Fprintf(w, "event: %s\n", event.Type)
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event.Data)
			flusher.Flush()
		}
	}
}
