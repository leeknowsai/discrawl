package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/steipete/discrawl/internal/web/webctx"
	analytictmpl "github.com/steipete/discrawl/internal/web/templates/analytics"
)

// daysCutoff returns a UTC timestamp string for N days ago, safe for parameterized SQL.
func daysCutoff(days int) string {
	return time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02T15:04:05")
}

// parseIntParam parses a query param as int, returning defaultVal if missing or invalid.
func parseIntParam(r *http.Request, key string, defaultVal int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return defaultVal
	}
	return v
}

// HandleAnalyticsDashboard renders the analytics page.
func HandleAnalyticsDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		guildID := chi.URLParam(r, "guildID")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = analytictmpl.Dashboard(guildID, guildID).Render(r.Context(), w)
	}
}

// HandleMessageVolume returns message counts per day for the last N days.
// GET /api/v1/g/{guildID}/stats/message-volume?days=30
func HandleMessageVolume() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		days := parseIntParam(r, "days", 30)

		cutoff := daysCutoff(days)
		rows, err := gs.ReadDB().QueryContext(r.Context(), `
			SELECT date(created_at) as day, COUNT(*) as cnt
			FROM messages
			WHERE deleted_at IS NULL AND created_at > ?
			GROUP BY day
			ORDER BY day
		`, cutoff)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		defer func() { _ = rows.Close() }()

		var labels []string
		var data []int
		for rows.Next() {
			var day string
			var cnt int
			if err := rows.Scan(&day, &cnt); err != nil {
				continue
			}
			labels = append(labels, day)
			data = append(data, cnt)
		}

		writeChartJSON(w, labels, "Messages", data, "rgba(99,102,241,0.8)")
	}
}

// HandleActivityHeatmap returns message counts per weekday/hour.
// GET /api/v1/g/{guildID}/stats/activity-heatmap?days=30
func HandleActivityHeatmap() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		days := parseIntParam(r, "days", 30)

		cutoff := daysCutoff(days)
		rows, err := gs.ReadDB().QueryContext(r.Context(), `
			SELECT cast(strftime('%w', created_at) as integer) as weekday,
			       cast(strftime('%H', created_at) as integer) as hour,
			       COUNT(*) as cnt
			FROM messages
			WHERE deleted_at IS NULL AND created_at > ?
			GROUP BY weekday, hour
		`, cutoff)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		defer func() { _ = rows.Close() }()

		type point struct {
			X int `json:"x"`
			Y int `json:"y"`
			V int `json:"v"`
		}
		var pts []point
		for rows.Next() {
			var weekday, hour, cnt int
			if err := rows.Scan(&weekday, &hour, &cnt); err != nil {
				continue
			}
			pts = append(pts, point{X: hour, Y: weekday, V: cnt})
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": pts})
	}
}

// HandleTopMembers returns top message authors.
// GET /api/v1/g/{guildID}/stats/top-members?limit=20&days=30
func HandleTopMembers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		days := parseIntParam(r, "days", 30)
		limit := parseIntParam(r, "limit", 20)

		cutoff := daysCutoff(days)
		rows, err := gs.ReadDB().QueryContext(r.Context(), `
			SELECT m.author_id,
			       COALESCE(mem.display_name, mem.nick, mem.username, m.author_id) as name,
			       COUNT(*) as cnt
			FROM messages m
			LEFT JOIN members mem ON mem.user_id = m.author_id AND mem.guild_id = m.guild_id
			WHERE m.deleted_at IS NULL AND m.created_at > ?
			GROUP BY m.author_id
			ORDER BY cnt DESC
			LIMIT ?
		`, cutoff, limit)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		defer func() { _ = rows.Close() }()

		var labels []string
		var data []int
		for rows.Next() {
			var authorID, name string
			var cnt int
			if err := rows.Scan(&authorID, &name, &cnt); err != nil {
				continue
			}
			labels = append(labels, name)
			data = append(data, cnt)
		}

		writeChartJSON(w, labels, "Messages", data, "rgba(16,185,129,0.8)")
	}
}

// HandleChannelActivity returns message counts per channel.
// GET /api/v1/g/{guildID}/stats/channel-activity?days=30
func HandleChannelActivity() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		days := parseIntParam(r, "days", 30)

		cutoff := daysCutoff(days)
		rows, err := gs.ReadDB().QueryContext(r.Context(), `
			SELECT m.channel_id,
			       COALESCE(c.name, m.channel_id) as name,
			       COUNT(*) as cnt
			FROM messages m
			LEFT JOIN channels c ON c.id = m.channel_id
			WHERE m.deleted_at IS NULL AND m.created_at > ?
			GROUP BY m.channel_id
			ORDER BY cnt DESC
			LIMIT 20
		`, cutoff)
		if err != nil {
			http.Error(w, "query failed", http.StatusInternalServerError)
			return
		}
		defer func() { _ = rows.Close() }()

		var labels []string
		var data []int
		for rows.Next() {
			var channelID, name string
			var cnt int
			if err := rows.Scan(&channelID, &name, &cnt); err != nil {
				continue
			}
			labels = append(labels, "#"+name)
			data = append(data, cnt)
		}

		writeChartJSON(w, labels, "Messages", data, "rgba(245,158,11,0.8)")
	}
}

// HandleOverviewStats returns guild-level aggregate stats.
// GET /api/v1/g/{guildID}/stats/overview
func HandleOverviewStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		stats, err := gs.GuildStats(r.Context())
		if err != nil {
			http.Error(w, "failed to load stats", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stats)
	}
}

// writeChartJSON writes a Chart.js compatible JSON response.
func writeChartJSON(w http.ResponseWriter, labels []string, datasetLabel string, data []int, color string) {
	if labels == nil {
		labels = []string{}
	}
	if data == nil {
		data = []int{}
	}
	payload := map[string]any{
		"labels": labels,
		"datasets": []map[string]any{
			{
				"label":           datasetLabel,
				"data":            data,
				"backgroundColor": color,
				"borderColor":     color,
				"borderWidth":     1,
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}
