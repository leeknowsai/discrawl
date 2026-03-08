// Package webctx provides shared context keys and accessors for the web layer,
// avoiding import cycles between the web, handlers, and auth packages.
package webctx

import (
	"context"

	"github.com/steipete/discrawl/internal/store"
)

type contextKey int

const (
	CtxKeyGuildStore contextKey = iota
	CtxKeyUserID
)

// WithGuildStore stores a GuildStore in the context.
func WithGuildStore(ctx context.Context, gs *store.GuildStore) context.Context {
	return context.WithValue(ctx, CtxKeyGuildStore, gs)
}

// GetGuildStore retrieves the GuildStore injected by TenantResolver.
func GetGuildStore(ctx context.Context) *store.GuildStore {
	gs, _ := ctx.Value(CtxKeyGuildStore).(*store.GuildStore)
	return gs
}

// WithUserID stores a user ID in the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, CtxKeyUserID, userID)
}

// GetUserID retrieves the authenticated user ID from context.
func GetUserID(ctx context.Context) string {
	id, _ := ctx.Value(CtxKeyUserID).(string)
	return id
}
