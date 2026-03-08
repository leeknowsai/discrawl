package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/web/webctx"
	guildtmpl "github.com/steipete/discrawl/internal/web/templates/guild"
)

// HandleGuildDashboard shows aggregate stats for the guild.
func HandleGuildDashboard(registry *store.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		guildID := chi.URLParam(r, "guildID")

		stats, err := gs.GuildStats(r.Context())
		if err != nil {
			http.Error(w, "failed to load stats", http.StatusInternalServerError)
			return
		}

		guildName := resolveGuildName(r, registry, guildID)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = guildtmpl.Dashboard(guildID, guildName, stats).Render(r.Context(), w)
	}
}

// HandleGuildSelector shows all guilds available in the registry.
func HandleGuildSelector(meta *store.MetaStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		guilds, err := meta.ListGuilds(r.Context())
		if err != nil {
			http.Error(w, "failed to load guilds", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = guildtmpl.Selector(guilds).Render(r.Context(), w)
	}
}

// HandleChannelSidebar returns the HTMX partial for the channel list.
func HandleChannelSidebar(registry *store.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		guildID := chi.URLParam(r, "guildID")

		channels, err := gs.Channels(r.Context(), guildID)
		if err != nil {
			http.Error(w, "failed to load channels", http.StatusInternalServerError)
			return
		}

		categories := buildCategories(channels)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = guildtmpl.ChannelSidebar(guildID, categories).Render(r.Context(), w)
	}
}

// buildCategories groups channels by parent_id into Category slices.
func buildCategories(channels []store.ChannelRow) []guildtmpl.Category {
	catNames := map[string]string{}
	for _, ch := range channels {
		if ch.Kind == "category" {
			catNames[ch.ID] = ch.Name
		}
	}

	catOrder := []string{""}
	groups := map[string][]store.ChannelRow{}
	seenParents := map[string]bool{"": true}
	for _, ch := range channels {
		if ch.Kind == "category" {
			continue
		}
		parent := ch.ParentID
		if !seenParents[parent] {
			seenParents[parent] = true
			catOrder = append(catOrder, parent)
		}
		groups[parent] = append(groups[parent], ch)
	}

	var out []guildtmpl.Category
	for _, parentID := range catOrder {
		chs, ok := groups[parentID]
		if !ok {
			continue
		}
		out = append(out, guildtmpl.Category{
			ID:       parentID,
			Name:     catNames[parentID],
			Channels: chs,
		})
	}
	return out
}

// resolveGuildName looks up a human-readable guild name from the meta store.
func resolveGuildName(r *http.Request, registry *store.Registry, guildID string) string {
	if registry.Meta() == nil {
		return guildID
	}
	guilds, err := registry.Meta().ListGuilds(r.Context())
	if err != nil {
		return guildID
	}
	for _, g := range guilds {
		if g.ID == guildID {
			return g.Name
		}
	}
	return guildID
}
