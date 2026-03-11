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

func setupGuildTestDB(t *testing.T) (*store.Registry, *store.GuildStore) {
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
	_, err = db.ExecContext(ctx, `INSERT INTO channels (id, guild_id, kind, name, parent_id, position, is_nsfw, is_archived, is_locked, is_private_thread, raw_json, updated_at) VALUES
		('cat1', 'test-guild', 'category', 'General', '', 0, 0, 0, 0, 0, '{}', datetime('now')),
		('ch1', 'test-guild', 'text', 'welcome', 'cat1', 1, 0, 0, 0, 0, '{}', datetime('now')),
		('ch2', 'test-guild', 'text', 'random', 'cat1', 2, 0, 0, 0, 0, '{}', datetime('now'))`)
	require.NoError(t, err)

	return reg, gs
}

func TestHandleGuildDashboard(t *testing.T) {
	reg, gs := setupGuildTestDB(t)

	handler := HandleGuildDashboard(reg)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild", nil)
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
		req := httptest.NewRequest("GET", "/g/test-guild", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleChannelSidebar(t *testing.T) {
	reg, gs := setupGuildTestDB(t)

	handler := HandleChannelSidebar(reg)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild/channels", nil)
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
		req := httptest.NewRequest("GET", "/g/test-guild/channels", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestBuildCategories(t *testing.T) {
	channels := []store.ChannelRow{
		{ID: "cat1", Kind: "category", Name: "General"},
		{ID: "ch1", Kind: "text", Name: "welcome", ParentID: "cat1"},
		{ID: "ch2", Kind: "text", Name: "random", ParentID: "cat1"},
		{ID: "ch3", Kind: "text", Name: "orphan", ParentID: ""},
	}

	categories := buildCategories(channels)

	require.Len(t, categories, 2) // One for empty parent, one for cat1

	// Find category for empty parent
	var orphanCat, generalCat *int
	for i, cat := range categories {
		if cat.ID == "" {
			idx := i
			orphanCat = &idx
		} else if cat.ID == "cat1" {
			idx := i
			generalCat = &idx
		}
	}

	require.NotNil(t, orphanCat)
	require.Len(t, categories[*orphanCat].Channels, 1)
	require.Equal(t, "orphan", categories[*orphanCat].Channels[0].Name)

	require.NotNil(t, generalCat)
	require.Equal(t, "General", categories[*generalCat].Name)
	require.Len(t, categories[*generalCat].Channels, 2)
}

func TestResolveGuildName(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	reg, err := store.NewRegistry(ctx, store.RegistryConfig{DataDir: tempDir})
	require.NoError(t, err)

	// Register a guild
	err = reg.Meta().RegisterGuild(ctx, store.MetaGuild{ID: "g1", Name: "Test Guild"})
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		name := resolveGuildName(req, reg, "g1")
		require.Equal(t, "Test Guild", name)
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		name := resolveGuildName(req, reg, "unknown")
		require.Equal(t, "unknown", name)
	})
}
