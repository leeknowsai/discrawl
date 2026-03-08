package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/web/webctx"
)

const exportRowLimit = 50000

// HandleExportMessages streams messages as CSV.
// GET /api/v1/g/{guildID}/export/messages?channel={id}&format=csv
func HandleExportMessages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gs := webctx.GetGuildStore(r.Context())
		if gs == nil {
			http.Error(w, "guild not found", http.StatusNotFound)
			return
		}
		guildID := chi.URLParam(r, "guildID")
		channelID := r.URL.Query().Get("channel")

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="messages-%s.csv"`, guildID))

		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"message_id", "channel_id", "channel_name", "author_id", "author_name", "content", "created_at"})

		msgs, err := gs.ListMessages(r.Context(), store.MessageListOptions{
			Channel:        channelID,
			Limit:          exportRowLimit,
			ExcludeDeleted: true,
			IncludeEmpty:   true,
		})
		if err != nil {
			// Headers already sent; best effort.
			cw.Flush()
			return
		}

		for _, msg := range msgs {
			_ = cw.Write([]string{
				msg.MessageID,
				msg.ChannelID,
				msg.ChannelName,
				msg.AuthorID,
				msg.AuthorName,
				msg.Content,
				msg.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			})
		}
		cw.Flush()
	}
}
