package store

import "context"

// DataStore is the interface satisfied by both Store (single-DB) and GuildStore (per-guild).
// The syncer and tail handler use this interface so they work with either mode.
type DataStore interface {
	UpsertGuild(ctx context.Context, guild GuildRecord) error
	UpsertChannel(ctx context.Context, channel ChannelRecord) error
	ReplaceMembers(ctx context.Context, guildID string, members []MemberRecord) error
	UpsertMember(ctx context.Context, member MemberRecord) error
	DeleteMember(ctx context.Context, guildID, userID string) error
	UpsertMessages(ctx context.Context, messages []MessageMutation) error
	MarkMessageDeleted(ctx context.Context, guildID, channelID, messageID string, payload any) error
	AppendMessageEvent(ctx context.Context, guildID, channelID, messageID, eventType string, payload any) error
	SetSyncState(ctx context.Context, scope, cursor string) error
	GetSyncState(ctx context.Context, scope string) (string, error)
	ChannelMessageBounds(ctx context.Context, channelID string) (string, string, error)
	Channels(ctx context.Context, guildID string) ([]ChannelRow, error)
}

// Compile-time interface checks.
var (
	_ DataStore = (*Store)(nil)
	_ DataStore = (*GuildStore)(nil)
)
