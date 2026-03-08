package web

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/web/webctx"
)

// RequestLogger returns a middleware that logs each request with slog.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			defer func() {
				logger.Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", ww.Status(),
					"bytes", ww.BytesWritten(),
					"duration", time.Since(start),
					"request_id", middleware.GetReqID(r.Context()),
				)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// TenantResolver extracts the guildID URL param, fetches the GuildStore from
// the registry, and injects it into the request context.
func TenantResolver(registry *store.Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if registry == nil {
				next.ServeHTTP(w, r)
				return
			}
			guildID := chi.URLParam(r, "guildID")
			if guildID == "" {
				next.ServeHTTP(w, r)
				return
			}
			// Check that the authenticated user has access to this guild.
			userID := webctx.GetUserID(r.Context())
			if userID != "" && userID != "dev" && registry.Meta() != nil {
				hasAccess, _ := registry.Meta().UserHasGuild(r.Context(), userID, guildID)
				if !hasAccess {
					http.Error(w, "forbidden", http.StatusForbidden)
					return
				}
			}

			gs, err := registry.Get(r.Context(), guildID)
			if err != nil {
				http.Error(w, "guild not found", http.StatusNotFound)
				return
			}
			defer registry.Release(guildID)
			ctx := webctx.WithGuildStore(r.Context(), gs)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetGuildStore retrieves the GuildStore injected by TenantResolver.
// Delegates to webctx to avoid import cycles in handlers.
func GetGuildStore(r *http.Request) *store.GuildStore {
	return webctx.GetGuildStore(r.Context())
}
