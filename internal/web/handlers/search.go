package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/web/webctx"
	searchtmpl "github.com/steipete/discrawl/internal/web/templates/search"
)

// HandleSearch renders the search page and processes queries.
func HandleSearch(registry *store.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		guildID := chi.URLParam(r, "guildID")

		q := r.URL.Query().Get("q")
		channelFilter := r.URL.Query().Get("in")
		authorFilter := r.URL.Query().Get("from")
		afterFilter := r.URL.Query().Get("after")
		beforeFilter := r.URL.Query().Get("before")

		var results []store.SearchResult
		if q != "" {
			var err error
			results, err = gs.SearchMessages(r.Context(), store.SearchOptions{
				Query:   q,
				Channel: channelFilter,
				Author:  authorFilter,
				Limit:   50,
			})
			if err != nil {
				http.Error(w, "search failed", http.StatusInternalServerError)
				return
			}
		}

		guildName := resolveGuildName(r, registry, guildID)

		// Build active filters
		var filters []searchtmpl.SearchFilter
		if authorFilter != "" {
			filters = append(filters, searchtmpl.SearchFilter{
				Type:  "from",
				Value: authorFilter,
				Label: "from: " + authorFilter,
			})
		}
		if channelFilter != "" {
			filters = append(filters, searchtmpl.SearchFilter{
				Type:  "in",
				Value: channelFilter,
				Label: "in: #" + channelFilter,
			})
		}
		if afterFilter != "" {
			filters = append(filters, searchtmpl.SearchFilter{
				Type:  "after",
				Value: afterFilter,
				Label: "after: " + afterFilter,
			})
		}
		if beforeFilter != "" {
			filters = append(filters, searchtmpl.SearchFilter{
				Type:  "before",
				Value: beforeFilter,
				Label: "before: " + beforeFilter,
			})
		}

		// TODO: Load recent searches from session or database
		var recentSearches []string

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// HTMX partial: return only results fragment.
		if r.Header.Get("HX-Request") == "true" {
			_ = searchtmpl.SearchResults(guildID, results, q).Render(r.Context(), w)
			return
		}

		_ = searchtmpl.Page(guildID, guildName, results, q, filters, recentSearches).Render(r.Context(), w)
	}
}
