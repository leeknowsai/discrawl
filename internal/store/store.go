package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const timeLayout = time.RFC3339Nano

type Store struct {
	db *sql.DB
}

type Status struct {
	DBPath             string    `json:"db_path"`
	GuildCount         int       `json:"guild_count"`
	ChannelCount       int       `json:"channel_count"`
	ThreadCount        int       `json:"thread_count"`
	MessageCount       int       `json:"message_count"`
	MemberCount        int       `json:"member_count"`
	EmbeddingBacklog   int       `json:"embedding_backlog"`
	LastSyncAt         time.Time `json:"last_sync_at,omitempty"`
	LastTailEventAt    time.Time `json:"last_tail_event_at,omitempty"`
	DefaultGuildID     string    `json:"default_guild_id,omitempty"`
	DefaultGuildName   string    `json:"default_guild_name,omitempty"`
	AccessibleGuildIDs []string  `json:"accessible_guild_ids,omitempty"`
}

type SearchOptions struct {
	Query    string
	GuildIDs []string
	Channel  string
	Author   string
	Limit    int
}

type SearchResult struct {
	MessageID   string    `json:"message_id"`
	GuildID     string    `json:"guild_id"`
	ChannelID   string    `json:"channel_id"`
	ChannelName string    `json:"channel_name"`
	AuthorID    string    `json:"author_id"`
	AuthorName  string    `json:"author_name"`
	Content     string    `json:"content"`
	CreatedAt   time.Time `json:"created_at"`
}

type MemberRow struct {
	GuildID     string    `json:"guild_id"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	GlobalName  string    `json:"global_name,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	Nick        string    `json:"nick,omitempty"`
	RoleIDsJSON string    `json:"role_ids_json"`
	Bot         bool      `json:"bot"`
	JoinedAt    time.Time `json:"joined_at,omitempty"`
}

type ChannelRow struct {
	ID               string    `json:"id"`
	GuildID          string    `json:"guild_id"`
	ParentID         string    `json:"parent_id,omitempty"`
	Kind             string    `json:"kind"`
	Name             string    `json:"name"`
	Topic            string    `json:"topic,omitempty"`
	Position         int       `json:"position"`
	IsNSFW           bool      `json:"is_nsfw"`
	IsArchived       bool      `json:"is_archived"`
	IsLocked         bool      `json:"is_locked"`
	IsPrivateThread  bool      `json:"is_private_thread"`
	ThreadParentID   string    `json:"thread_parent_id,omitempty"`
	ArchiveTimestamp time.Time `json:"archive_timestamp,omitempty"`
}

func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite is single-writer; keep one shared connection so concurrent callers queue
	// instead of contending on separate writer connections.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`create table if not exists guilds (
			id text primary key,
			name text not null,
			icon text,
			raw_json text not null,
			updated_at text not null
		);`,
		`create table if not exists channels (
			id text primary key,
			guild_id text not null,
			parent_id text,
			kind text not null,
			name text not null,
			topic text,
			position integer,
			is_nsfw integer not null default 0,
			is_archived integer not null default 0,
			is_locked integer not null default 0,
			is_private_thread integer not null default 0,
			thread_parent_id text,
			archive_timestamp text,
			raw_json text not null,
			updated_at text not null
		);`,
		`create table if not exists members (
			guild_id text not null,
			user_id text not null,
			username text not null,
			global_name text,
			display_name text,
			nick text,
			discriminator text,
			avatar text,
			bot integer not null default 0,
			joined_at text,
			role_ids_json text not null,
			raw_json text not null,
			updated_at text not null,
			primary key (guild_id, user_id)
		);`,
		`create table if not exists messages (
			id text primary key,
			guild_id text not null,
			channel_id text not null,
			author_id text,
			message_type integer not null,
			created_at text not null,
			edited_at text,
			deleted_at text,
			content text not null,
			normalized_content text not null,
			reply_to_message_id text,
			pinned integer not null default 0,
			has_attachments integer not null default 0,
			raw_json text not null,
			updated_at text not null
		);`,
		`create table if not exists message_events (
			event_id integer primary key autoincrement,
			guild_id text not null,
			channel_id text not null,
			message_id text not null,
			event_type text not null,
			event_at text not null,
			payload_json text not null
		);`,
		`create table if not exists sync_state (
			scope text primary key,
			cursor text,
			updated_at text not null
		);`,
		`create table if not exists embedding_jobs (
			message_id text primary key,
			state text not null,
			attempts integer not null default 0,
			updated_at text not null
		);`,
		`create virtual table if not exists message_fts using fts5(
			message_id unindexed,
			guild_id unindexed,
			channel_id unindexed,
			author_id unindexed,
			author_name,
			channel_name,
			content
		);`,
		`create index if not exists idx_channels_guild_id on channels(guild_id);`,
		`create index if not exists idx_members_guild_id on members(guild_id);`,
		`create index if not exists idx_messages_channel_id on messages(channel_id);`,
		`create index if not exists idx_messages_guild_id on messages(guild_id);`,
		`create index if not exists idx_events_message_id on message_events(message_id);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
