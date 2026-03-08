package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// registryEntry tracks an open GuildStore with reference counting and LRU metadata.
type registryEntry struct {
	store    *GuildStore
	refs     atomic.Int64
	lastUsed atomic.Int64 // unix nano timestamp
}

// Registry manages multiple per-guild SQLite databases with lazy-open,
// LRU eviction, and reference counting to protect in-flight stores.
type Registry struct {
	mu      sync.RWMutex
	stores  map[string]*registryEntry
	dataDir string
	metaDB  *MetaStore
	maxOpen int
	onWrite WriteHookFunc
}

// RegistryConfig configures a Registry.
type RegistryConfig struct {
	DataDir string // base directory containing guilds/ subdirectory
	MaxOpen int    // max concurrent open guild DBs (default 50)
	OnWrite WriteHookFunc
}

// NewRegistry creates a new registry for per-guild databases.
// It also opens the meta.db for guild registry and sync state.
func NewRegistry(ctx context.Context, cfg RegistryConfig) (*Registry, error) {
	if cfg.MaxOpen <= 0 {
		cfg.MaxOpen = 50
	}
	guildsDir := filepath.Join(cfg.DataDir, "guilds")
	if err := os.MkdirAll(guildsDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir guilds dir: %w", err)
	}
	metaPath := filepath.Join(cfg.DataDir, "meta.db")
	meta, err := OpenMetaStore(ctx, metaPath)
	if err != nil {
		return nil, fmt.Errorf("open meta.db: %w", err)
	}
	return &Registry{
		stores:  make(map[string]*registryEntry),
		dataDir: cfg.DataDir,
		metaDB:  meta,
		maxOpen: cfg.MaxOpen,
		onWrite: cfg.OnWrite,
	}, nil
}

// Get returns a GuildStore for the given guild ID, opening it if necessary.
// The caller MUST call Release(guildID) when done to decrement the ref count.
func (r *Registry) Get(ctx context.Context, guildID string) (*GuildStore, error) {
	if !isValidSnowflake(guildID) {
		return nil, fmt.Errorf("invalid guild ID: %q", guildID)
	}

	// Fast path: already open. Increment ref count while holding the read lock
	// to prevent a race with evictLocked closing the store between unlock and Add.
	r.mu.RLock()
	entry, ok := r.stores[guildID]
	if ok {
		entry.refs.Add(1)
		entry.lastUsed.Store(time.Now().UnixNano())
	}
	r.mu.RUnlock()
	if ok {
		return entry.store, nil
	}

	// Slow path: need to open.
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if entry, ok := r.stores[guildID]; ok {
		entry.refs.Add(1)
		entry.lastUsed.Store(time.Now().UnixNano())
		return entry.store, nil
	}

	// Evict idle stores if at capacity.
	if len(r.stores) >= r.maxOpen {
		r.evictLocked()
	}

	dbPath := filepath.Join(r.dataDir, "guilds", guildID+".db")
	gs, err := OpenGuildStore(ctx, dbPath, guildID)
	if err != nil {
		return nil, fmt.Errorf("open guild store %s: %w", guildID, err)
	}
	if r.onWrite != nil {
		gs.SetWriteHook(r.onWrite)
	}

	entry = &registryEntry{store: gs}
	entry.refs.Store(1)
	entry.lastUsed.Store(time.Now().UnixNano())
	r.stores[guildID] = entry
	return gs, nil
}

// Release decrements the reference count for a guild store.
func (r *Registry) Release(guildID string) {
	r.mu.RLock()
	entry, ok := r.stores[guildID]
	r.mu.RUnlock()
	if ok {
		entry.refs.Add(-1)
	}
}

// evictLocked closes the least recently used guild stores that have zero refs.
// Must be called with r.mu held for writing.
func (r *Registry) evictLocked() {
	// Find entries with zero refs, sorted by last used time.
	type candidate struct {
		guildID  string
		lastUsed int64
	}
	var candidates []candidate
	for guildID, entry := range r.stores {
		if entry.refs.Load() <= 0 {
			candidates = append(candidates, candidate{guildID: guildID, lastUsed: entry.lastUsed.Load()})
		}
	}

	// Sort by lastUsed ascending (oldest first).
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].lastUsed < candidates[i].lastUsed {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Evict until we're below maxOpen or no more candidates.
	evictCount := len(r.stores) - r.maxOpen + 1
	for i := 0; i < evictCount && i < len(candidates); i++ {
		guildID := candidates[i].guildID
		entry := r.stores[guildID]
		// Re-check refs in case someone acquired a ref between our check and now.
		if entry.refs.Load() > 0 {
			continue
		}
		_ = entry.store.Close()
		delete(r.stores, guildID)
	}
}

// ListGuildIDs returns all guild IDs that have DB files on disk.
func (r *Registry) ListGuildIDs() ([]string, error) {
	guildsDir := filepath.Join(r.dataDir, "guilds")
	entries, err := os.ReadDir(guildsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) == ".db" {
			id := name[:len(name)-3]
			if isValidSnowflake(id) {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}

// Meta returns the MetaStore for cross-guild metadata.
func (r *Registry) Meta() *MetaStore {
	return r.metaDB
}

// Close closes all open guild stores and the meta DB.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for guildID, entry := range r.stores {
		if err := entry.store.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(r.stores, guildID)
	}
	if r.metaDB != nil {
		if err := r.metaDB.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// isValidSnowflake checks that a guild ID is a numeric snowflake.
func isValidSnowflake(id string) bool {
	if len(id) == 0 || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
