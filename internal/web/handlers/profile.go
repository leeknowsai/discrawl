package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/web/webctx"
	membertmpl "github.com/steipete/discrawl/internal/web/templates/members"
)

// HandleMemberProfile shows a member's profile with their recent messages.
// GET /app/g/{guildID}/members/{userID}
func HandleMemberProfile() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		guildID := chi.URLParam(r, "guildID")
		userID := chi.URLParam(r, "userID")

		// Look up member info.
		members, err := gs.Members(r.Context(), userID, 1)
		if err != nil || len(members) == 0 {
			http.Error(w, "member not found", http.StatusNotFound)
			return
		}
		member := members[0]

		// Fetch recent messages by this author.
		msgs, err := gs.ListMessages(r.Context(), store.MessageListOptions{
			Author:         userID,
			Limit:          100,
			ExcludeDeleted: true,
		})
		if err != nil {
			msgs = nil
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = membertmpl.Profile(guildID, guildID, member, msgs).Render(r.Context(), w)
	}
}
