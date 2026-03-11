package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/web/webctx"
	"github.com/stretchr/testify/require"
)

func setupSearchTestDB(t *testing.T) (*store.Registry, *store.GuildStore) {
	t.Helper()
	ctx := context.Background()
	tempDir := t.TempDir()
	reg, err := store.NewRegistry(ctx, store.RegistryConfig{DataDir: tempDir})
	require.NoError(t, err)

	dbPath := tempDir + "/guilds/test-guild.db"
	gs, err := store.OpenGuildStore(ctx, dbPath, "test-guild")
	require.NoError(t, err)

	// Insert test data
	db := gs.DB()
	_, err = db.ExecContext(ctx, `INSERT INTO messages (id, guild_id, channel_id, author_id, message_type, content, normalized_content, created_at, raw_json, updated_at) VALUES
		('m1', 'test-guild', 'ch1', 'u1', 0, 'hello world', 'hello world', datetime('now'), '{}', datetime('now')),
		('m2', 'test-guild', 'ch1', 'u2', 0, 'test message', 'test message', datetime('now'), '{}', datetime('now'))`)
	require.NoError(t, err)

	return reg, gs
}

func TestHandleSearch(t *testing.T) {
	reg, gs := setupSearchTestDB(t)

	handler := HandleSearch(reg)

	t.Run("success without query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild/search", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "test-guild")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	})

	t.Run("success with query", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild/search?q=hello", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "test-guild")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("success with filters", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild/search?q=test&in=general&from=alice", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "test-guild")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("htmx partial request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild/search?q=hello", nil)
		req.Header.Set("HX-Request", "true")
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "test-guild")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	})

	t.Run("no guild store", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild/search", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}
