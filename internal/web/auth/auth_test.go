package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/steipete/discrawl/internal/web/webctx"
	"golang.org/x/oauth2"
)

func TestNewOAuth2Config(t *testing.T) {
	cfg := OAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURI:  "http://localhost/callback",
	}

	oauthCfg := NewOAuth2Config(cfg)

	if oauthCfg.ClientID != cfg.ClientID {
		t.Errorf("ClientID = %q, want %q", oauthCfg.ClientID, cfg.ClientID)
	}
	if oauthCfg.ClientSecret != cfg.ClientSecret {
		t.Errorf("ClientSecret = %q, want %q", oauthCfg.ClientSecret, cfg.ClientSecret)
	}
	if oauthCfg.RedirectURL != cfg.RedirectURI {
		t.Errorf("RedirectURL = %q, want %q", oauthCfg.RedirectURL, cfg.RedirectURI)
	}
	if len(oauthCfg.Scopes) != 2 {
		t.Errorf("Scopes length = %d, want 2", len(oauthCfg.Scopes))
	}
	if oauthCfg.Endpoint.AuthURL != discordAuthURL {
		t.Errorf("AuthURL = %q, want %q", oauthCfg.Endpoint.AuthURL, discordAuthURL)
	}
	if oauthCfg.Endpoint.TokenURL != discordTokenURL {
		t.Errorf("TokenURL = %q, want %q", oauthCfg.Endpoint.TokenURL, discordTokenURL)
	}
}

func TestRequireAuth_DevMode(t *testing.T) {
	// Set dev mode
	os.Setenv("DISCRAWL_DEV", "1")
	defer os.Unsetenv("DISCRAWL_DEV")

	sm := scs.New()
	middleware := RequireAuth(sm)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := webctx.GetUserID(r.Context())
		if userID != "dev" {
			t.Errorf("UserID in dev mode = %q, want 'dev'", userID)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireAuth_NoSession_RedirectsToLogin(t *testing.T) {
	os.Unsetenv("DISCRAWL_DEV")

	sm := scs.New()
	middleware := RequireAuth(sm)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler without session")
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	// Load session context (but don't set userID)
	ctx, _ := sm.Load(req.Context(), "")
	req = req.Clone(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	location := rec.Header().Get("Location")
	if location != "/auth/login" {
		t.Errorf("Location = %q, want '/auth/login'", location)
	}
}

func TestRequireAuth_NoSession_HTMX_Returns401WithHeader(t *testing.T) {
	os.Unsetenv("DISCRAWL_DEV")

	sm := scs.New()
	middleware := RequireAuth(sm)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Should not reach handler without session")
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("HX-Request", "true")
	// Load session context (but don't set userID)
	ctx, _ := sm.Load(req.Context(), "")
	req = req.Clone(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	hxRedirect := rec.Header().Get("HX-Redirect")
	if hxRedirect != "/auth/login" {
		t.Errorf("HX-Redirect = %q, want '/auth/login'", hxRedirect)
	}
}

func TestRequireAuth_WithValidSession(t *testing.T) {
	os.Unsetenv("DISCRAWL_DEV")

	sm := scs.New()
	middleware := RequireAuth(sm)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := webctx.GetUserID(r.Context())
		if userID != "user123" {
			t.Errorf("UserID = %q, want 'user123'", userID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Create a request with session
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)

	// Load and save session to set userID
	ctx, _ := sm.Load(req.Context(), "")
	sm.Put(ctx, sessionKeyUserID, "user123")

	// Create new request with session cookie
	sessionReq := req.Clone(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, sessionReq)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestHandleLogin(t *testing.T) {
	sm := scs.New()
	oauthCfg := &oauth2.Config{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/callback",
		Scopes:       []string{"identify", "guilds"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://discord.com/api/oauth2/authorize",
			TokenURL: "https://discord.com/api/oauth2/token",
		},
	}

	handler := HandleLogin(sm, oauthCfg)

	req := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	ctx, _ := sm.Load(req.Context(), "")
	req = req.Clone(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}

	location := rec.Header().Get("Location")
	if !strings.Contains(location, "discord.com/api/oauth2/authorize") {
		t.Errorf("Location should contain Discord auth URL, got %q", location)
	}
	if !strings.Contains(location, "state=") {
		t.Error("Location should contain state parameter")
	}
}

func TestHandleLogout(t *testing.T) {
	sm := scs.New()
	handler := HandleLogout(sm)

	req := httptest.NewRequest(http.MethodGet, "/auth/logout", nil)
	ctx, _ := sm.Load(req.Context(), "")
	sm.Put(ctx, sessionKeyUserID, "user123")
	req = req.Clone(ctx)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusSeeOther)
	}

	location := rec.Header().Get("Location")
	if location != "/" {
		t.Errorf("Location = %q, want '/'", location)
	}
}

func TestGenerateState(t *testing.T) {
	state1, err := generateState()
	if err != nil {
		t.Fatalf("generateState() error = %v", err)
	}

	state2, err := generateState()
	if err != nil {
		t.Fatalf("generateState() error = %v", err)
	}

	// Should be 32 characters (16 bytes hex encoded)
	if len(state1) != 32 {
		t.Errorf("state length = %d, want 32", len(state1))
	}

	// Should be unique
	if state1 == state2 {
		t.Error("generateState() should produce unique values")
	}

	// Should be valid hex
	for _, c := range state1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("state contains invalid hex character: %c", c)
		}
	}
}

// Note: fetchDiscordUser and fetchDiscordGuilds are not tested here because
// they depend on the discordAPIBase const which cannot be modified for testing.
// These functions are integration-tested as part of the OAuth callback flow.

func TestGetUserID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	// No user ID in context
	userID := GetUserID(req)
	if userID != "" {
		t.Errorf("GetUserID() = %q, want ''", userID)
	}

	// With user ID in context
	ctx := webctx.WithUserID(req.Context(), "user789")
	req = req.Clone(ctx)

	userID = GetUserID(req)
	if userID != "user789" {
		t.Errorf("GetUserID() = %q, want 'user789'", userID)
	}
}
