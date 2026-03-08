package handlers

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/web/webctx"
	membertmpl "github.com/steipete/discrawl/internal/web/templates/members"
)

// HandleMemberList renders the member list page with optional search.
func HandleMemberList(registry *store.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		guildID := chi.URLParam(r, "guildID")

		q := r.URL.Query().Get("q")
		limit := 100
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if n, err := strconv.Atoi(lStr); err == nil && n > 0 {
				limit = n
			}
		}

		members, err := gs.Members(r.Context(), q, limit)
		if err != nil {
			http.Error(w, "failed to load members", http.StatusInternalServerError)
			return
		}

		guildName := resolveGuildName(r, registry, guildID)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// HTMX partial request: return only the results fragment.
		if r.Header.Get("HX-Request") == "true" {
			_ = membertmpl.MemberResults(members).Render(r.Context(), w)
			return
		}

		_ = membertmpl.List(guildID, guildName, members, q).Render(r.Context(), w)
	}
}
