package auth

import (
	"net/http"
	"os"

	"github.com/alexedwards/scs/v2"
	"github.com/steipete/discrawl/internal/web/webctx"
)

// SessionUser represents the logged-in user stored in context.
type SessionUser struct {
	ID       string
	Username string
	Avatar   string
}

// RequireAuth middleware checks session for userID; redirects to /auth/login if absent.
// For HTMX requests it returns 401 with HX-Redirect header instead.
func RequireAuth(sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Dev mode: bypass auth when DISCRAWL_DEV=1.
			if os.Getenv("DISCRAWL_DEV") == "1" {
				ctx := webctx.WithUserID(r.Context(), "dev")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			userID := sm.GetString(r.Context(), sessionKeyUserID)
			if userID == "" {
				if r.Header.Get("HX-Request") == "true" {
					w.Header().Set("HX-Redirect", "/auth/login")
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
				return
			}
			ctx := webctx.WithUserID(r.Context(), userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUserID extracts user ID from context (set by RequireAuth).
func GetUserID(r *http.Request) string {
	return webctx.GetUserID(r.Context())
}
