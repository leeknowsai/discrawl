package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// WriteEvent represents a store mutation for SSE fan-out.
type WriteEvent struct {
	Type    string // "message_create", "message_update", "message_delete", "member_update", "member_delete"
	GuildID string
	Data    any
}

// WriteHookFunc is called after successful writes for SSE integration.
type WriteHookFunc func(guildID string, event WriteEvent)

// GuildStore wraps a single guild's SQLite DB with separate read connection
// and optional write hook for live event broadcasting.
type GuildStore struct {
	db      *sql.DB
	readDB  *sql.DB
	guildID string
	path    string
	onWrite WriteHookFunc
}

// OpenGuildStore opens or creates a per-guild SQLite database.
func OpenGuildStore(ctx context.Context, path, guildID string) (*GuildStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir guild db dir: %w", err)
	}
	if err := ensureDBFile(path); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)&_pragma=busy_timeout(5000)",
		path,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open guild sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping guild sqlite: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0o600)
	}

	// Open separate read-only connection for web handlers.
	readDSN := fmt.Sprintf(
		"file:%s?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)",
		path,
	)
	readDB, err := sql.Open("sqlite", readDSN)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open guild read sqlite: %w", err)
	}
	// Allow multiple concurrent readers.
	readDB.SetMaxOpenConns(4)
	readDB.SetMaxIdleConns(2)

	gs := &GuildStore{
		db:      db,
		readDB:  readDB,
		guildID: guildID,
		path:    path,
	}
	if err := gs.migrate(ctx); err != nil {
		_ = readDB.Close()
		_ = db.Close()
		return nil, err
	}
	return gs, nil
}

// SetWriteHook sets the callback invoked after successful writes.
func (gs *GuildStore) SetWriteHook(fn WriteHookFunc) {
	gs.onWrite = fn
}

// notifyWrite fires the write hook if set. Should be non-blocking.
func (gs *GuildStore) notifyWrite(event WriteEvent) {
	if gs.onWrite != nil {
		gs.onWrite(gs.guildID, event)
	}
}

// Close closes both write and read DB connections.
func (gs *GuildStore) Close() error {
	if gs == nil {
		return nil
	}
	var firstErr error
	if gs.readDB != nil {
		if err := gs.readDB.Close(); err != nil {
			firstErr = err
		}
	}
	if gs.db != nil {
		if err := gs.db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// GuildID returns the guild ID this store manages.
func (gs *GuildStore) GuildID() string {
	return gs.guildID
}

// DB returns the write database connection.
func (gs *GuildStore) DB() *sql.DB {
	return gs.db
}

// ReadDB returns the read-only database connection for web handlers.
func (gs *GuildStore) ReadDB() *sql.DB {
	return gs.readDB
}

// migrate runs schema migrations on the guild DB.
func (gs *GuildStore) migrate(ctx context.Context) error {
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
		`create table if not exists message_attachments (
			attachment_id text primary key,
			message_id text not null,
			guild_id text not null,
			channel_id text not null,
			author_id text,
			filename text not null,
			content_type text,
			size integer not null default 0,
			url text,
			proxy_url text,
			text_content text not null default '',
			updated_at text not null
		);`,
		`create table if not exists mention_events (
			event_id integer primary key autoincrement,
			message_id text not null,
			guild_id text not null,
			channel_id text not null,
			author_id text,
			target_type text not null,
			target_id text not null,
			target_name text not null default '',
			event_at text not null
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
		`create index if not exists idx_attachments_message_id on message_attachments(message_id);`,
		`create index if not exists idx_attachments_channel_id on message_attachments(channel_id);`,
		`create index if not exists idx_mentions_message_id on mention_events(message_id);`,
		`create index if not exists idx_mentions_target on mention_events(target_type, target_id, event_at);`,
		`create index if not exists idx_mentions_author on mention_events(author_id, event_at);`,
	}
	for _, stmt := range stmts {
		if _, err := gs.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate guild db: %w", err)
		}
	}
	return gs.ensureFTSRowIDs(ctx)
}

// ensureFTSRowIDs checks if the FTS index needs rebuilding.
func (gs *GuildStore) ensureFTSRowIDs(ctx context.Context) error {
	var version sql.NullString
	err := gs.db.QueryRowContext(ctx, `
		select cursor from sync_state where scope = 'schema:message_fts_rowid_version'
	`).Scan(&version)
	if err == nil && version.String == messageFTSVersion {
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("check fts schema version: %w", err)
	}
	if err := gs.rebuildFTS(ctx); err != nil {
		return err
	}
	_, err = gs.db.ExecContext(ctx, `
		insert into sync_state(scope, cursor, updated_at)
		values(?, ?, ?)
		on conflict(scope) do update set
			cursor=excluded.cursor,
			updated_at=excluded.updated_at
	`, "schema:message_fts_rowid_version", messageFTSVersion, time.Now().UTC().Format(timeLayout))
	if err != nil {
		return fmt.Errorf("stamp fts schema version: %w", err)
	}
	return nil
}

// rebuildFTS drops and recreates the FTS5 index.
func (gs *GuildStore) rebuildFTS(ctx context.Context) error {
	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	if _, err := tx.ExecContext(ctx, `drop table if exists message_fts`); err != nil {
		return fmt.Errorf("drop message_fts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		create virtual table message_fts using fts5(
			message_id unindexed,
			guild_id unindexed,
			channel_id unindexed,
			author_id unindexed,
			author_name,
			channel_name,
			content
		)
	`); err != nil {
		return fmt.Errorf("create message_fts: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		select
			m.id, m.guild_id, m.channel_id,
			coalesce(m.author_id, ''),
			coalesce(
				json_extract(m.raw_json, '$.member.nick'),
				json_extract(m.raw_json, '$.author.global_name'),
				json_extract(m.raw_json, '$.author.username'),
				''
			),
			coalesce(c.name, ''),
			m.normalized_content
		from messages m
		left join channels c on c.id = m.channel_id
		order by cast(m.id as integer)
	`)
	if err != nil {
		return fmt.Errorf("query fts rebuild rows: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stmt, err := tx.PrepareContext(ctx, `
		insert into message_fts(
			rowid, message_id, guild_id, channel_id, author_id, author_name, channel_name, content
		) values(?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare fts rebuild: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for rows.Next() {
		var messageID, guildID, channelID, authorID, authorName, channelName, content string
		if err := rows.Scan(&messageID, &guildID, &channelID, &authorID, &authorName, &channelName, &content); err != nil {
			return fmt.Errorf("scan fts rebuild row: %w", err)
		}
		rowID, ok := messageFTSRowID(messageID)
		if !ok {
			continue
		}
		if _, err := stmt.ExecContext(ctx, rowID, messageID, guildID, channelID, nullable(authorID), authorName, channelName, content); err != nil {
			return fmt.Errorf("insert fts rebuild row: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate fts rebuild rows: %w", err)
	}
	return tx.Commit()
}

// GuildStats returns aggregate counts for this guild's DB.
func (gs *GuildStore) GuildStats(ctx context.Context) (GuildStats, error) {
	var stats GuildStats
	queries := []struct {
		query  string
		target *int
	}{
		{`select count(*) from messages where deleted_at is null`, &stats.MessageCount},
		{`select count(*) from members`, &stats.MemberCount},
		{`select count(*) from channels where kind not like 'thread_%'`, &stats.ChannelCount},
		{`select count(*) from channels where kind like 'thread_%'`, &stats.ThreadCount},
	}
	db := gs.readDB
	for _, q := range queries {
		if err := db.QueryRowContext(ctx, q.query).Scan(q.target); err != nil {
			return GuildStats{}, err
		}
	}
	var lastMsg sql.NullString
	_ = db.QueryRowContext(ctx, `select max(created_at) from messages where deleted_at is null`).Scan(&lastMsg)
	stats.LastMessageAt = parseTime(lastMsg.String)
	return stats, nil
}

// ListMessages queries messages from this guild's DB with cursor pagination.
func (gs *GuildStore) ListMessages(ctx context.Context, opts MessageListOptions) ([]MessageRow, error) {
	args := []any{}
	clauses := []string{"1=1"}
	if channel := normalizeChannelFilter(opts.Channel); channel != "" {
		clauses = append(clauses, "(m.channel_id = ? or c.name = ? or c.name like ?)")
		args = append(args, channel, channel, "%"+channel+"%")
	}
	if author := strings.TrimSpace(opts.Author); author != "" {
		clauses = append(clauses, `(m.author_id = ? or coalesce(mem.username, '') = ? or coalesce(mem.display_name, '') = ? or coalesce(mem.username, '') like ? or coalesce(mem.display_name, '') like ? or json_extract(m.raw_json, '$.author.username') = ?)`)
		args = append(args, author, author, author, "%"+author+"%", "%"+author+"%", author)
	}
	if !opts.Since.IsZero() {
		clauses = append(clauses, "m.created_at >= ?")
		args = append(args, opts.Since.UTC().Format(timeLayout))
	}
	if !opts.Before.IsZero() {
		clauses = append(clauses, "m.created_at < ?")
		args = append(args, opts.Before.UTC().Format(timeLayout))
	}
	if opts.BeforeID != "" {
		clauses = append(clauses, "m.id < ?")
		args = append(args, opts.BeforeID)
	}
	if opts.ExcludeDeleted {
		clauses = append(clauses, "m.deleted_at is null")
	}
	if !opts.IncludeEmpty {
		clauses = append(clauses, "trim(coalesce(m.normalized_content, '')) <> ''")
	}

	orderClause := "m.created_at asc, m.id asc"
	if opts.BeforeID != "" {
		orderClause = "m.id desc"
	}

	query := `
		select
			m.id, m.guild_id, m.channel_id, coalesce(c.name, ''),
			coalesce(m.author_id, ''),
			coalesce(
				nullif(mem.display_name, ''), nullif(mem.nick, ''),
				nullif(mem.global_name, ''), nullif(mem.username, ''),
				nullif(json_extract(m.raw_json, '$.author.global_name'), ''),
				nullif(json_extract(m.raw_json, '$.author.username'), ''), ''
			),
			case when trim(coalesce(m.content, '')) <> '' then m.content else m.normalized_content end,
			m.created_at, coalesce(m.reply_to_message_id, ''),
			m.has_attachments, m.pinned
		from messages m
		left join channels c on c.id = m.channel_id
		left join members mem on mem.guild_id = m.guild_id and mem.user_id = m.author_id
		where ` + strings.Join(clauses, " and ") + `
		order by ` + orderClause
	if opts.Limit > 0 {
		query += ` limit ?`
		args = append(args, opts.Limit)
	}

	rows, err := gs.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []MessageRow
	for rows.Next() {
		var row MessageRow
		var created string
		var hasAttachments, pinned int
		if err := rows.Scan(
			&row.MessageID, &row.GuildID, &row.ChannelID, &row.ChannelName,
			&row.AuthorID, &row.AuthorName, &row.Content, &created,
			&row.ReplyToMessage, &hasAttachments, &pinned,
		); err != nil {
			return nil, err
		}
		row.CreatedAt = parseTime(created)
		row.HasAttachments = hasAttachments == 1
		row.Pinned = pinned == 1
		out = append(out, row)
	}
	return out, rows.Err()
}

// SearchMessages performs FTS5 search on this guild's DB.
func (gs *GuildStore) SearchMessages(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return nil, nil
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	args := []any{normalizeFTSQuery(opts.Query)}
	clauses := []string{"message_fts match ?"}
	if strings.TrimSpace(opts.Channel) != "" {
		clauses = append(clauses, "(message_fts.channel_id = ? or message_fts.channel_name like ?)")
		args = append(args, opts.Channel, "%"+opts.Channel+"%")
	}
	if strings.TrimSpace(opts.Author) != "" {
		clauses = append(clauses, "(message_fts.author_id = ? or message_fts.author_name like ?)")
		args = append(args, opts.Author, "%"+opts.Author+"%")
	}
	if !opts.IncludeEmpty {
		clauses = append(clauses, "trim(coalesce(m.normalized_content, '')) <> ''")
	}
	// Always exclude deleted in web context.
	clauses = append(clauses, "m.deleted_at is null")
	args = append(args, opts.Limit)
	query := `
		select
			m.id, m.guild_id, m.channel_id, coalesce(c.name, ''),
			coalesce(m.author_id, ''), coalesce(message_fts.author_name, ''),
			case when trim(coalesce(m.content, '')) <> '' then m.content else m.normalized_content end,
			m.created_at
		from message_fts
		join messages m on m.id = message_fts.message_id
		left join channels c on c.id = m.channel_id
		where ` + strings.Join(clauses, " and ") + `
		order by bm25(message_fts), m.created_at desc
		limit ?
	`
	rows, err := gs.readDB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SearchResult
	for rows.Next() {
		var row SearchResult
		var created string
		if err := rows.Scan(&row.MessageID, &row.GuildID, &row.ChannelID, &row.ChannelName, &row.AuthorID, &row.AuthorName, &row.Content, &created); err != nil {
			return nil, err
		}
		row.CreatedAt = parseTime(created)
		out = append(out, row)
	}
	return out, rows.Err()
}

// Members queries members from this guild's read DB.
func (gs *GuildStore) Members(ctx context.Context, query string, limit int) ([]MemberRow, error) {
	if limit <= 0 {
		limit = 100
	}
	args := []any{}
	clauses := []string{"1=1"}
	if query != "" {
		clauses = append(clauses, `(username like ? or coalesce(display_name, '') like ? or coalesce(nick, '') like ? or user_id = ?)`)
		args = append(args, "%"+query+"%", "%"+query+"%", "%"+query+"%", query)
	}
	args = append(args, limit)
	rows, err := gs.readDB.QueryContext(ctx, `
		select guild_id, user_id, username, coalesce(global_name, ''), coalesce(display_name, ''),
		       coalesce(nick, ''), role_ids_json, bot, coalesce(joined_at, '')
		from members
		where `+strings.Join(clauses, " and ")+`
		order by coalesce(display_name, nick, username), username
		limit ?
	`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MemberRow
	for rows.Next() {
		var row MemberRow
		var joined string
		if err := rows.Scan(&row.GuildID, &row.UserID, &row.Username, &row.GlobalName, &row.DisplayName, &row.Nick, &row.RoleIDsJSON, &row.Bot, &joined); err != nil {
			return nil, err
		}
		row.JoinedAt = parseTime(joined)
		out = append(out, row)
	}
	return out, rows.Err()
}

// Channels queries channels from this guild's read DB.
// guildID is accepted for DataStore interface compatibility but ignored (guild-scoped).
func (gs *GuildStore) Channels(ctx context.Context, _ string) ([]ChannelRow, error) {
	rows, err := gs.readDB.QueryContext(ctx, `
		select id, guild_id, coalesce(parent_id, ''), kind, name, coalesce(topic, ''), position,
		       is_nsfw, is_archived, is_locked, is_private_thread, coalesce(thread_parent_id, ''), coalesce(archive_timestamp, '')
		from channels
		order by position, name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ChannelRow
	for rows.Next() {
		var row ChannelRow
		var nsfw, archived, locked, priv int
		var archiveTS string
		if err := rows.Scan(&row.ID, &row.GuildID, &row.ParentID, &row.Kind, &row.Name, &row.Topic, &row.Position, &nsfw, &archived, &locked, &priv, &row.ThreadParentID, &archiveTS); err != nil {
			return nil, err
		}
		row.IsNSFW = nsfw == 1
		row.IsArchived = archived == 1
		row.IsLocked = locked == 1
		row.IsPrivateThread = priv == 1
		row.ArchiveTimestamp = parseTime(archiveTS)
		out = append(out, row)
	}
	return out, rows.Err()
}

// --- Write methods that delegate to the underlying db and fire write hooks ---

// UpsertGuild upserts a guild record.
func (gs *GuildStore) UpsertGuild(ctx context.Context, guild GuildRecord) error {
	now := time.Now().UTC().Format(timeLayout)
	_, err := gs.db.ExecContext(ctx, `
		insert into guilds(id, name, icon, raw_json, updated_at)
		values(?, ?, ?, ?, ?)
		on conflict(id) do update set
			name=excluded.name, icon=excluded.icon,
			raw_json=excluded.raw_json, updated_at=excluded.updated_at
	`, guild.ID, guild.Name, guild.Icon, guild.RawJSON, now)
	return err
}

// UpsertChannel upserts a channel record.
func (gs *GuildStore) UpsertChannel(ctx context.Context, channel ChannelRecord) error {
	now := time.Now().UTC().Format(timeLayout)
	_, err := gs.db.ExecContext(ctx, `
		insert into channels(
			id, guild_id, parent_id, kind, name, topic, position, is_nsfw,
			is_archived, is_locked, is_private_thread, thread_parent_id,
			archive_timestamp, raw_json, updated_at
		) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(id) do update set
			guild_id=excluded.guild_id, parent_id=excluded.parent_id,
			kind=excluded.kind, name=excluded.name, topic=excluded.topic,
			position=excluded.position, is_nsfw=excluded.is_nsfw,
			is_archived=excluded.is_archived, is_locked=excluded.is_locked,
			is_private_thread=excluded.is_private_thread,
			thread_parent_id=excluded.thread_parent_id,
			archive_timestamp=excluded.archive_timestamp,
			raw_json=excluded.raw_json, updated_at=excluded.updated_at
	`, channel.ID, channel.GuildID, channel.ParentID, channel.Kind, channel.Name, channel.Topic, channel.Position,
		boolInt(channel.IsNSFW), boolInt(channel.IsArchived), boolInt(channel.IsLocked), boolInt(channel.IsPrivateThread),
		channel.ThreadParentID, nullable(channel.ArchiveTimestamp), channel.RawJSON, now)
	return err
}

// ReplaceMembers replaces all members for the guild.
func (gs *GuildStore) ReplaceMembers(ctx context.Context, guildID string, members []MemberRecord) error {
	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `delete from members where guild_id = ?`, guildID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(timeLayout)
	stmt, err := tx.PrepareContext(ctx, `
		insert into members(
			guild_id, user_id, username, global_name, display_name, nick, discriminator,
			avatar, bot, joined_at, role_ids_json, raw_json, updated_at
		) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, member := range members {
		if _, err := stmt.ExecContext(ctx, member.GuildID, member.UserID, member.Username, nullable(member.GlobalName),
			nullable(member.DisplayName), nullable(member.Nick), nullable(member.Discriminator), nullable(member.Avatar),
			boolInt(member.Bot), nullable(member.JoinedAt), member.RoleIDsJSON, member.RawJSON, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	gs.notifyWrite(WriteEvent{Type: "member_update", GuildID: guildID, Data: len(members)})
	return nil
}

// UpsertMember upserts a single member.
func (gs *GuildStore) UpsertMember(ctx context.Context, member MemberRecord) error {
	now := time.Now().UTC().Format(timeLayout)
	_, err := gs.db.ExecContext(ctx, `
		insert into members(
			guild_id, user_id, username, global_name, display_name, nick, discriminator,
			avatar, bot, joined_at, role_ids_json, raw_json, updated_at
		) values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(guild_id, user_id) do update set
			username=excluded.username, global_name=excluded.global_name,
			display_name=excluded.display_name, nick=excluded.nick,
			discriminator=excluded.discriminator, avatar=excluded.avatar,
			bot=excluded.bot, joined_at=excluded.joined_at,
			role_ids_json=excluded.role_ids_json, raw_json=excluded.raw_json,
			updated_at=excluded.updated_at
	`, member.GuildID, member.UserID, member.Username, nullable(member.GlobalName), nullable(member.DisplayName),
		nullable(member.Nick), nullable(member.Discriminator), nullable(member.Avatar), boolInt(member.Bot),
		nullable(member.JoinedAt), member.RoleIDsJSON, member.RawJSON, now)
	if err != nil {
		return err
	}
	gs.notifyWrite(WriteEvent{Type: "member_update", GuildID: gs.guildID, Data: member})
	return nil
}

// DeleteMember deletes a member.
func (gs *GuildStore) DeleteMember(ctx context.Context, guildID, userID string) error {
	_, err := gs.db.ExecContext(ctx, `delete from members where guild_id = ? and user_id = ?`, guildID, userID)
	if err != nil {
		return err
	}
	gs.notifyWrite(WriteEvent{Type: "member_delete", GuildID: guildID, Data: userID})
	return nil
}

// UpsertMessages upserts a batch of messages with attachments and mentions.
func (gs *GuildStore) UpsertMessages(ctx context.Context, messages []MessageMutation) error {
	if len(messages) == 0 {
		return nil
	}
	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	for _, message := range messages {
		if err := upsertMessageTx(ctx, tx, message.Record, message.Options); err != nil {
			return err
		}
		if err := replaceAttachmentsTx(ctx, tx, message.Record.ID, message.Attachments); err != nil {
			return err
		}
		if err := replaceMentionEventsTx(ctx, tx, message.Record.ID, message.Mentions); err != nil {
			return err
		}
		if message.Options.AppendEvent && message.EventType != "" {
			if err := appendEventTx(ctx, tx, message.Record.GuildID, message.Record.ChannelID, message.Record.ID, message.EventType, message.PayloadJSON); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, msg := range messages {
		gs.notifyWrite(WriteEvent{Type: "message_create", GuildID: gs.guildID, Data: msg.Record})
	}
	return nil
}

// MarkMessageDeleted marks a message as deleted.
func (gs *GuildStore) MarkMessageDeleted(ctx context.Context, guildID, channelID, messageID string, payload any) error {
	tx, err := gs.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)
	now := time.Now().UTC().Format(timeLayout)
	if _, err := tx.ExecContext(ctx, `update messages set deleted_at = ?, updated_at = ? where id = ?`, now, now, messageID); err != nil {
		return err
	}
	body, err := marshalJSON(payload)
	if err != nil {
		return err
	}
	if err := appendEventTx(ctx, tx, guildID, channelID, messageID, "delete", string(body)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	gs.notifyWrite(WriteEvent{Type: "message_delete", GuildID: guildID, Data: messageID})
	return nil
}

// AppendMessageEvent appends a message event.
func (gs *GuildStore) AppendMessageEvent(ctx context.Context, guildID, channelID, messageID, eventType string, payload any) error {
	body, err := marshalJSON(payload)
	if err != nil {
		return err
	}
	_, err = gs.db.ExecContext(ctx, `
		insert into message_events(guild_id, channel_id, message_id, event_type, event_at, payload_json)
		values(?, ?, ?, ?, ?, ?)
	`, guildID, channelID, messageID, eventType, time.Now().UTC().Format(timeLayout), string(body))
	return err
}

// SetSyncState sets a sync state checkpoint.
func (gs *GuildStore) SetSyncState(ctx context.Context, scope, cursor string) error {
	_, err := gs.db.ExecContext(ctx, `
		insert into sync_state(scope, cursor, updated_at)
		values(?, ?, ?)
		on conflict(scope) do update set
			cursor=excluded.cursor,
			updated_at=excluded.updated_at
	`, scope, cursor, time.Now().UTC().Format(timeLayout))
	return err
}

// GetSyncState retrieves a sync state cursor.
func (gs *GuildStore) GetSyncState(ctx context.Context, scope string) (string, error) {
	var cursor sql.NullString
	err := gs.db.QueryRowContext(ctx, `select cursor from sync_state where scope = ?`, scope).Scan(&cursor)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return cursor.String, nil
}

// ChannelMessageBounds returns the oldest and newest message IDs for a channel.
func (gs *GuildStore) ChannelMessageBounds(ctx context.Context, channelID string) (string, string, error) {
	var oldest, newest sql.NullString
	if err := gs.db.QueryRowContext(ctx, `
		select min(id), max(id) from messages where channel_id = ?
	`, channelID).Scan(&oldest, &newest); err != nil {
		return "", "", err
	}
	return oldest.String, newest.String, nil
}
