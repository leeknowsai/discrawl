package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/web/webctx"
	"github.com/stretchr/testify/require"
)

func setupAnalyticsTestDB(t *testing.T) (*store.Registry, *store.GuildStore) {
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

	// Insert channels
	_, err = db.ExecContext(ctx, `INSERT INTO channels (id, guild_id, kind, name, raw_json, updated_at) VALUES
		('ch1', 'test-guild', 'text', 'general', '{}', datetime('now')),
		('ch2', 'test-guild', 'text', 'random', '{}', datetime('now'))`)
	require.NoError(t, err)

	// Insert members
	_, err = db.ExecContext(ctx, `INSERT INTO members (guild_id, user_id, username, display_name, role_ids_json, raw_json, updated_at) VALUES
		('test-guild', 'u1', 'alice', 'Alice', '[]', '{}', datetime('now')),
		('test-guild', 'u2', 'bob', 'Bob', '[]', '{}', datetime('now'))`)
	require.NoError(t, err)

	// Insert messages from last 30 days
	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		created := now.AddDate(0, 0, -i).Format("2006-01-02T15:04:05")
		authorID := "u1"
		channelID := "ch1"
		if i%2 == 0 {
			authorID = "u2"
			channelID = "ch2"
		}
		_, err = db.ExecContext(ctx, `INSERT INTO messages (id, guild_id, channel_id, author_id, message_type, content, normalized_content, created_at, raw_json, updated_at) VALUES
			(?, 'test-guild', ?, ?, 0, ?, ?, ?, '{}', datetime('now'))`,
			"msg"+string(rune('0'+i)), channelID, authorID, "test content", "test content", created)
		require.NoError(t, err)
	}

	return reg, gs
}

func TestHandleAnalyticsDashboard(t *testing.T) {
	reg, gs := setupAnalyticsTestDB(t)

	handler := HandleAnalyticsDashboard(reg)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild/analytics", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("guildID", "test-guild")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	})

	t.Run("no guild store in context", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/g/test-guild/analytics", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleMessageVolume(t *testing.T) {
	_, gs := setupAnalyticsTestDB(t)

	handler := HandleMessageVolume()

	t.Run("success with default days", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/message-volume", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var result map[string]interface{}
		err := json.NewDecoder(rr.Body).Decode(&result)
		require.NoError(t, err)
		require.Contains(t, result, "labels")
		require.Contains(t, result, "datasets")
	})

	t.Run("success with custom days", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/message-volume?days=7", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("no guild store", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/message-volume", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleActivityHeatmap(t *testing.T) {
	_, gs := setupAnalyticsTestDB(t)

	handler := HandleActivityHeatmap()

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/activity-heatmap", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var result map[string]interface{}
		err := json.NewDecoder(rr.Body).Decode(&result)
		require.NoError(t, err)
		require.Contains(t, result, "data")
	})

	t.Run("with custom days parameter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/activity-heatmap?days=14", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("no guild store", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/activity-heatmap", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleTopMembers(t *testing.T) {
	_, gs := setupAnalyticsTestDB(t)

	handler := HandleTopMembers()

	t.Run("success with defaults", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/top-members", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var result map[string]interface{}
		err := json.NewDecoder(rr.Body).Decode(&result)
		require.NoError(t, err)
		require.Contains(t, result, "labels")
		require.Contains(t, result, "datasets")
	})

	t.Run("success with custom limit and days", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/top-members?limit=5&days=7", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("invalid parameters use defaults", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/top-members?limit=invalid&days=-1", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("no guild store", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/top-members", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleChannelActivity(t *testing.T) {
	_, gs := setupAnalyticsTestDB(t)

	handler := HandleChannelActivity()

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/channel-activity", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var result map[string]interface{}
		err := json.NewDecoder(rr.Body).Decode(&result)
		require.NoError(t, err)
		require.Contains(t, result, "labels")
		require.Contains(t, result, "datasets")
	})

	t.Run("with custom days", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/channel-activity?days=14", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("no guild store", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/channel-activity", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestHandleOverviewStats(t *testing.T) {
	_, gs := setupAnalyticsTestDB(t)

	handler := HandleOverviewStats()

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/overview", nil)
		req = req.WithContext(webctx.WithGuildStore(req.Context(), gs))

		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusOK, rr.Code)
		require.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var stats store.GuildStats
		err := json.NewDecoder(rr.Body).Decode(&stats)
		require.NoError(t, err)
		require.Equal(t, 10, stats.MessageCount)
		require.Equal(t, 2, stats.ChannelCount)
		require.Equal(t, 2, stats.MemberCount)
	})

	t.Run("no guild store", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/g/test-guild/stats/overview", nil)
		rr := httptest.NewRecorder()
		handler(rr, req)

		require.Equal(t, http.StatusNotFound, rr.Code)
	})
}

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		name       string
		queryParam string
		defaultVal int
		expected   int
	}{
		{"missing param", "", 10, 10},
		{"valid param", "5", 10, 5},
		{"invalid param", "abc", 10, 10},
		{"negative param", "-5", 10, 10},
		{"zero param", "0", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?key="+tt.queryParam, nil)
			result := parseIntParam(req, "key", tt.defaultVal)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{5, "5"},
		{999, "999"},
		{1000, "1K"},
		{1500, "1K"},
		{999999, "999K"},
		{1000000, "1M"},
		{2500000, "2M"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatNumber(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestDaysCutoff(t *testing.T) {
	result := daysCutoff(30)
	require.NotEmpty(t, result)

	// Verify format is ISO8601
	_, err := time.Parse("2006-01-02T15:04:05", result)
	require.NoError(t, err)
}
