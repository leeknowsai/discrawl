package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// MigrateOptions configures the DB split migration.
type MigrateOptions struct {
	SourceDB string // path to original discrawl.db
	DataDir  string // target directory (will contain guilds/ and meta.db)
	Logger   *slog.Logger
	DryRun   bool
}

// MigrateResult holds migration outcome stats.
type MigrateResult struct {
	GuildCount   int
	GuildResults []GuildMigrateResult
}

// GuildMigrateResult holds per-guild migration stats.
type GuildMigrateResult struct {
	GuildID      string
	GuildName    string
	Messages     int
	Members      int
	Channels     int
	Events       int
	Attachments  int
	Mentions     int
}

// MigrateSplitDB splits a single discrawl.db into per-guild SQLite files + meta.db.
// Idempotent: safe to re-run (uses INSERT OR IGNORE / ON CONFLICT).
func MigrateSplitDB(ctx context.Context, opts MigrateOptions) (MigrateResult, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	logger := opts.Logger

	// Open source DB read-only.
	srcDSN := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)&_pragma=temp_store(MEMORY)", opts.SourceDB)
	srcDB, err := sql.Open("sqlite", srcDSN)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("open source db: %w", err)
	}
	defer func() { _ = srcDB.Close() }()

	if err := srcDB.PingContext(ctx); err != nil {
		return MigrateResult{}, fmt.Errorf("ping source db: %w", err)
	}

	// Discover guilds.
	guildRows, err := srcDB.QueryContext(ctx, `select id, name, coalesce(icon, ''), raw_json from guilds order by id`)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("query guilds: %w", err)
	}
	type guildInfo struct {
		ID, Name, Icon, RawJSON string
	}
	var guilds []guildInfo
	for guildRows.Next() {
		var g guildInfo
		if err := guildRows.Scan(&g.ID, &g.Name, &g.Icon, &g.RawJSON); err != nil {
			_ = guildRows.Close()
			return MigrateResult{}, err
		}
		guilds = append(guilds, g)
	}
	_ = guildRows.Close()
	if err := guildRows.Err(); err != nil {
		return MigrateResult{}, err
	}

	logger.Info("discovered guilds", "count", len(guilds))

	if opts.DryRun {
		result := MigrateResult{GuildCount: len(guilds)}
		for _, g := range guilds {
			result.GuildResults = append(result.GuildResults, GuildMigrateResult{
				GuildID: g.ID, GuildName: g.Name,
			})
		}
		return result, nil
	}

	// Create target directories.
	guildsDir := filepath.Join(opts.DataDir, "guilds")
	if err := os.MkdirAll(guildsDir, 0o755); err != nil {
		return MigrateResult{}, fmt.Errorf("mkdir guilds dir: %w", err)
	}

	// Open meta.db.
	metaPath := filepath.Join(opts.DataDir, "meta.db")
	meta, err := OpenMetaStore(ctx, metaPath)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("open meta store: %w", err)
	}
	defer func() { _ = meta.Close() }()

	result := MigrateResult{GuildCount: len(guilds)}

	for _, guild := range guilds {
		logger.Info("migrating guild", "id", guild.ID, "name", guild.Name)

		gr, err := migrateGuild(ctx, srcDB, guild.ID, guildsDir, meta)
		if err != nil {
			return result, fmt.Errorf("migrate guild %s: %w", guild.ID, err)
		}
		gr.GuildName = guild.Name

		// Register in meta.db.
		if err := meta.RegisterGuild(ctx, MetaGuild{
			ID:     guild.ID,
			Name:   guild.Name,
			Icon:   guild.Icon,
			DBPath: filepath.Join("guilds", guild.ID+".db"),
		}); err != nil {
			return result, fmt.Errorf("register guild %s: %w", guild.ID, err)
		}

		logger.Info("guild migrated",
			"id", guild.ID,
			"messages", gr.Messages,
			"members", gr.Members,
			"channels", gr.Channels,
		)
		result.GuildResults = append(result.GuildResults, gr)
	}

	// Migrate sync_state to meta.db (guild-scoped entries).
	if err := migrateSyncState(ctx, srcDB, meta); err != nil {
		return result, fmt.Errorf("migrate sync state: %w", err)
	}

	logger.Info("migration complete", "guilds", result.GuildCount)
	return result, nil
}

// migrateGuild copies all data for a single guild from source to a per-guild DB.
func migrateGuild(ctx context.Context, srcDB *sql.DB, guildID, guildsDir string, meta *MetaStore) (GuildMigrateResult, error) {
	result := GuildMigrateResult{GuildID: guildID}
	dbPath := filepath.Join(guildsDir, guildID+".db")

	gs, err := OpenGuildStore(ctx, dbPath, guildID)
	if err != nil {
		return result, err
	}
	defer func() { _ = gs.Close() }()

	// Migrate guilds table (just the one guild).
	if err := copyRows(ctx, srcDB, gs.db, "guilds",
		`select id, name, coalesce(icon, ''), raw_json, updated_at from guilds where id = ?`,
		`insert or ignore into guilds(id, name, icon, raw_json, updated_at) values(?, ?, ?, ?, ?)`,
		[]any{guildID},
	); err != nil {
		return result, fmt.Errorf("copy guilds: %w", err)
	}

	// Migrate channels.
	n, err := copyRowsCount(ctx, srcDB, gs.db, "channels",
		`select id, guild_id, coalesce(parent_id, ''), kind, name, coalesce(topic, ''), position,
		        is_nsfw, is_archived, is_locked, is_private_thread, coalesce(thread_parent_id, ''),
		        coalesce(archive_timestamp, ''), raw_json, updated_at
		 from channels where guild_id = ?`,
		`insert or ignore into channels(id, guild_id, parent_id, kind, name, topic, position,
		        is_nsfw, is_archived, is_locked, is_private_thread, thread_parent_id,
		        archive_timestamp, raw_json, updated_at)
		 values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{guildID}, 15,
	)
	if err != nil {
		return result, fmt.Errorf("copy channels: %w", err)
	}
	result.Channels = n

	// Migrate members.
	n, err = copyRowsCount(ctx, srcDB, gs.db, "members",
		`select guild_id, user_id, username, global_name, display_name, nick, discriminator,
		        avatar, bot, joined_at, role_ids_json, raw_json, updated_at
		 from members where guild_id = ?`,
		`insert or ignore into members(guild_id, user_id, username, global_name, display_name, nick,
		        discriminator, avatar, bot, joined_at, role_ids_json, raw_json, updated_at)
		 values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{guildID}, 13,
	)
	if err != nil {
		return result, fmt.Errorf("copy members: %w", err)
	}
	result.Members = n

	// Migrate messages.
	n, err = copyRowsCount(ctx, srcDB, gs.db, "messages",
		`select id, guild_id, channel_id, author_id, message_type, created_at, edited_at, deleted_at,
		        content, normalized_content, reply_to_message_id, pinned, has_attachments, raw_json, updated_at
		 from messages where guild_id = ?`,
		`insert or ignore into messages(id, guild_id, channel_id, author_id, message_type, created_at,
		        edited_at, deleted_at, content, normalized_content, reply_to_message_id, pinned,
		        has_attachments, raw_json, updated_at)
		 values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{guildID}, 15,
	)
	if err != nil {
		return result, fmt.Errorf("copy messages: %w", err)
	}
	result.Messages = n

	// Migrate message_events.
	n, err = copyRowsCount(ctx, srcDB, gs.db, "message_events",
		`select guild_id, channel_id, message_id, event_type, event_at, payload_json
		 from message_events where guild_id = ?`,
		`insert into message_events(guild_id, channel_id, message_id, event_type, event_at, payload_json)
		 values(?, ?, ?, ?, ?, ?)`,
		[]any{guildID}, 6,
	)
	if err != nil {
		return result, fmt.Errorf("copy message_events: %w", err)
	}
	result.Events = n

	// Migrate message_attachments.
	n, err = copyRowsCount(ctx, srcDB, gs.db, "message_attachments",
		`select attachment_id, message_id, guild_id, channel_id, author_id, filename,
		        content_type, size, url, proxy_url, text_content, updated_at
		 from message_attachments where guild_id = ?`,
		`insert or ignore into message_attachments(attachment_id, message_id, guild_id, channel_id,
		        author_id, filename, content_type, size, url, proxy_url, text_content, updated_at)
		 values(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{guildID}, 12,
	)
	if err != nil {
		return result, fmt.Errorf("copy message_attachments: %w", err)
	}
	result.Attachments = n

	// Migrate mention_events.
	n, err = copyRowsCount(ctx, srcDB, gs.db, "mention_events",
		`select message_id, guild_id, channel_id, author_id, target_type, target_id, target_name, event_at
		 from mention_events where guild_id = ?`,
		`insert into mention_events(message_id, guild_id, channel_id, author_id, target_type, target_id, target_name, event_at)
		 values(?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{guildID}, 8,
	)
	if err != nil {
		return result, fmt.Errorf("copy mention_events: %w", err)
	}
	result.Mentions = n

	// Migrate per-guild sync_state (channel scopes stay in guild DB).
	if err := copyRows(ctx, srcDB, gs.db, "sync_state",
		`select scope, cursor, updated_at from sync_state where scope like 'channel:%'`,
		`insert or ignore into sync_state(scope, cursor, updated_at) values(?, ?, ?)`,
		nil,
	); err != nil {
		return result, fmt.Errorf("copy sync_state: %w", err)
	}

	// Rebuild FTS index for this guild DB.
	if err := gs.rebuildFTS(ctx); err != nil {
		return result, fmt.Errorf("rebuild fts: %w", err)
	}
	// Stamp the FTS version.
	_, _ = gs.db.ExecContext(ctx, `
		insert into sync_state(scope, cursor, updated_at)
		values(?, ?, ?)
		on conflict(scope) do update set cursor=excluded.cursor, updated_at=excluded.updated_at
	`, "schema:message_fts_rowid_version", messageFTSVersion, time.Now().UTC().Format(timeLayout))

	return result, nil
}

// migrateSyncState copies global sync state entries to meta.db.
func migrateSyncState(ctx context.Context, srcDB *sql.DB, meta *MetaStore) error {
	rows, err := srcDB.QueryContext(ctx,
		`select scope, cursor from sync_state where scope not like 'channel:%' and scope not like 'schema:%'`,
	)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var scope, cursor string
		if err := rows.Scan(&scope, &cursor); err != nil {
			return err
		}
		// Global scopes go under guild_id="" in meta.
		if err := meta.SetSyncState(ctx, "", scope, cursor); err != nil {
			return err
		}
	}
	return rows.Err()
}

// copyRows copies rows from source to destination using the given queries.
func copyRows(ctx context.Context, srcDB, dstDB *sql.DB, table, selectQ, insertQ string, selectArgs []any) error {
	_, err := copyRowsCount(ctx, srcDB, dstDB, table, selectQ, insertQ, selectArgs, 0)
	return err
}

// copyRowsCount copies rows and returns the count. colCount is the number of columns per row.
func copyRowsCount(ctx context.Context, srcDB, dstDB *sql.DB, table, selectQ, insertQ string, selectArgs []any, colCount int) (int, error) {
	rows, err := srcDB.QueryContext(ctx, selectQ, selectArgs...)
	if err != nil {
		return 0, fmt.Errorf("query %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	// Detect column count from result if not provided.
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	if colCount == 0 {
		colCount = len(cols)
	}

	tx, err := dstDB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollback(tx)

	stmt, err := tx.PrepareContext(ctx, insertQ)
	if err != nil {
		return 0, fmt.Errorf("prepare insert %s: %w", table, err)
	}
	defer func() { _ = stmt.Close() }()

	count := 0
	for rows.Next() {
		values := make([]any, colCount)
		ptrs := make([]any, colCount)
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return count, fmt.Errorf("scan %s: %w", table, err)
		}
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return count, fmt.Errorf("insert %s: %w", table, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return count, err
	}
	return count, tx.Commit()
}
