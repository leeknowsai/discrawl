package sse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestBrokerServeHTTP(t *testing.T) {
	t.Run("success - receives events", func(t *testing.T) {
		broker := NewBroker()

		req := httptest.NewRequest("GET", "/sse/guild-1", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "guild-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		// Create a context with timeout to close the connection
		ctx, cancel := context.WithTimeout(req.Context(), 50*time.Millisecond)
		defer cancel()
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()

		// Publish an event in the background
		go func() {
			time.Sleep(10 * time.Millisecond)
			broker.Publish("guild-1", Event{ID: "evt-1", Type: "message", Data: "test data"})
		}()

		broker.ServeHTTP(rr, req)

		body := rr.Body.String()
		require.Contains(t, body, ": connected")
		require.Contains(t, body, "id: evt-1")
		require.Contains(t, body, "event: message")
		require.Contains(t, body, "data: test data")
	})

	t.Run("missing guildID", func(t *testing.T) {
		broker := NewBroker()

		req := httptest.NewRequest("GET", "/sse", nil)
		rr := httptest.NewRecorder()

		broker.ServeHTTP(rr, req)

		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Contains(t, rr.Body.String(), "missing guildID")
	})

	t.Run("sets correct headers", func(t *testing.T) {
		broker := NewBroker()

		req := httptest.NewRequest("GET", "/sse/guild-1", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "guild-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Millisecond)
		defer cancel()
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		broker.ServeHTTP(rr, req)

		require.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))
		require.Equal(t, "no-cache", rr.Header().Get("Cache-Control"))
		require.Equal(t, "keep-alive", rr.Header().Get("Connection"))
		require.Equal(t, "no", rr.Header().Get("X-Accel-Buffering"))
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		broker := NewBroker()

		req := httptest.NewRequest("GET", "/sse/guild-1", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "guild-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		ctx, cancel := context.WithCancel(req.Context())
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()

		// Cancel context immediately
		cancel()

		broker.ServeHTTP(rr, req)

		// Should have sent initial connection message before context cancellation
		body := rr.Body.String()
		require.Contains(t, body, ": connected")
	})

	t.Run("sends event without ID", func(t *testing.T) {
		broker := NewBroker()

		req := httptest.NewRequest("GET", "/sse/guild-1", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "guild-1")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		ctx, cancel := context.WithTimeout(req.Context(), 50*time.Millisecond)
		defer cancel()
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()

		go func() {
			time.Sleep(10 * time.Millisecond)
			// Event without ID
			broker.Publish("guild-1", Event{Type: "notification", Data: "hello"})
		}()

		broker.ServeHTTP(rr, req)

		body := rr.Body.String()
		require.Contains(t, body, "event: notification")
		require.Contains(t, body, "data: hello")
		require.False(t, strings.Contains(body, "id:"), "should not contain id field")
	})
}
