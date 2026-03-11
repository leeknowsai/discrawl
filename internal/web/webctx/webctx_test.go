package webctx

import (
	"context"
	"testing"

	"github.com/steipete/discrawl/internal/store"
)

func TestWithGuildStoreAndGet(t *testing.T) {
	ctx := context.Background()

	// Initially should return nil
	gs := GetGuildStore(ctx)
	if gs != nil {
		t.Errorf("GetGuildStore() on empty context should return nil, got %v", gs)
	}

	// Create a mock GuildStore
	mockStore := &store.GuildStore{}

	// Add to context
	ctx = WithGuildStore(ctx, mockStore)

	// Retrieve it
	retrieved := GetGuildStore(ctx)
	if retrieved != mockStore {
		t.Error("GetGuildStore() should return the same store that was set")
	}
}

func TestWithUserIDAndGet(t *testing.T) {
	ctx := context.Background()

	// Initially should return empty string
	userID := GetUserID(ctx)
	if userID != "" {
		t.Errorf("GetUserID() on empty context should return '', got %q", userID)
	}

	// Add user ID to context
	testUserID := "user123"
	ctx = WithUserID(ctx, testUserID)

	// Retrieve it
	retrieved := GetUserID(ctx)
	if retrieved != testUserID {
		t.Errorf("GetUserID() = %q, want %q", retrieved, testUserID)
	}
}

func TestContextChaining(t *testing.T) {
	ctx := context.Background()

	// Add both GuildStore and UserID
	mockStore := &store.GuildStore{}
	testUserID := "user456"

	ctx = WithGuildStore(ctx, mockStore)
	ctx = WithUserID(ctx, testUserID)

	// Both should be retrievable
	gs := GetGuildStore(ctx)
	if gs != mockStore {
		t.Error("GuildStore not preserved in chained context")
	}

	uid := GetUserID(ctx)
	if uid != testUserID {
		t.Errorf("UserID not preserved in chained context: got %q, want %q", uid, testUserID)
	}
}

func TestContextOverwrite(t *testing.T) {
	ctx := context.Background()

	// Set initial user ID
	ctx = WithUserID(ctx, "user1")

	// Overwrite with new user ID
	ctx = WithUserID(ctx, "user2")

	// Should get the new one
	uid := GetUserID(ctx)
	if uid != "user2" {
		t.Errorf("GetUserID() after overwrite = %q, want %q", uid, "user2")
	}
}

func TestGetWithWrongTypeInContext(t *testing.T) {
	ctx := context.Background()

	// Manually insert wrong type for GuildStore key
	ctx = context.WithValue(ctx, CtxKeyGuildStore, "not-a-guild-store")

	// Should return nil, not panic
	gs := GetGuildStore(ctx)
	if gs != nil {
		t.Error("GetGuildStore() with wrong type should return nil")
	}

	// Same for UserID
	ctx = context.WithValue(ctx, CtxKeyUserID, 12345) // int instead of string

	uid := GetUserID(ctx)
	if uid != "" {
		t.Error("GetUserID() with wrong type should return empty string")
	}
}

func TestNilGuildStore(t *testing.T) {
	ctx := context.Background()

	// Explicitly set nil GuildStore
	ctx = WithGuildStore(ctx, nil)

	// Should return nil
	gs := GetGuildStore(ctx)
	if gs != nil {
		t.Error("GetGuildStore() with nil store should return nil")
	}
}

func TestEmptyUserID(t *testing.T) {
	ctx := context.Background()

	// Set empty string as user ID
	ctx = WithUserID(ctx, "")

	// Should return empty string
	uid := GetUserID(ctx)
	if uid != "" {
		t.Errorf("GetUserID() with empty string = %q, want ''", uid)
	}
}
