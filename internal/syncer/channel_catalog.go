package syncer

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/steipete/discrawl/internal/store"
)

func (s *Syncer) channelList(ctx context.Context, guildID string, requested []string) ([]*discordgo.Channel, bool, error) {
	if len(requested) > 0 && s.store != nil {
		rows, err := s.store.Channels(ctx, guildID)
		if err != nil {
			return nil, false, err
		}
		if selected := selectStoredChannels(rows, requested); len(selected) > 0 {
			return selected, true, nil
		}
	}
	channels, err := s.client.GuildChannels(ctx, guildID)
	if err != nil {
		return nil, false, fmt.Errorf("fetch channels for guild %s: %w", guildID, err)
	}
	allChannels := make(map[string]*discordgo.Channel, len(channels))
	for _, channel := range channels {
		allChannels[channel.ID] = channel
	}
	for _, channel := range channels {
		if !isThreadParent(channel) {
			continue
		}
		active, err := s.client.ThreadsActive(ctx, channel.ID)
		if err == nil {
			for _, thread := range active {
				allChannels[thread.ID] = thread
			}
		}
		for _, private := range []bool{false, true} {
			archived, err := s.client.ThreadsArchived(ctx, channel.ID, private)
			if err != nil {
				s.logger.Warn("thread archive crawl failed", "channel_id", channel.ID, "private", private, "err", err)
				continue
			}
			for _, thread := range archived {
				allChannels[thread.ID] = thread
			}
		}
	}
	return mapsToSlice(allChannels), false, nil
}

func mapsToSlice(in map[string]*discordgo.Channel) []*discordgo.Channel {
	out := make([]*discordgo.Channel, 0, len(in))
	for _, channel := range in {
		out = append(out, channel)
	}
	sortChannels(out)
	return out
}

func selectStoredChannels(rows []store.ChannelRow, requested []string) []*discordgo.Channel {
	if len(rows) == 0 || len(requested) == 0 {
		return nil
	}
	set := makeGuildSet(requested)
	out := make([]*discordgo.Channel, 0, len(requested))
	for _, row := range rows {
		if _, ok := set[row.ID]; !ok {
			continue
		}
		channelType := channelTypeFromKind(row.Kind)
		var threadMeta *discordgo.ThreadMetadata
		if strings.HasPrefix(row.Kind, "thread_") {
			threadMeta = &discordgo.ThreadMetadata{
				Archived:         row.IsArchived,
				Locked:           row.IsLocked,
				ArchiveTimestamp: row.ArchiveTimestamp,
			}
		}
		out = append(out, &discordgo.Channel{
			ID:             row.ID,
			GuildID:        row.GuildID,
			ParentID:       row.ParentID,
			Name:           row.Name,
			Topic:          row.Topic,
			Position:       row.Position,
			NSFW:           row.IsNSFW,
			Type:           channelType,
			ThreadMetadata: threadMeta,
		})
	}
	sortChannels(out)
	return out
}

func sortChannels(channels []*discordgo.Channel) {
	slices.SortFunc(channels, func(a, b *discordgo.Channel) int {
		switch {
		case a.Position < b.Position:
			return -1
		case a.Position > b.Position:
			return 1
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
}

func isThreadParent(channel *discordgo.Channel) bool {
	if channel == nil {
		return false
	}
	switch channel.Type {
	case discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews, discordgo.ChannelTypeGuildForum:
		return true
	default:
		return false
	}
}

func isMessageChannel(channel *discordgo.Channel) bool {
	if channel == nil {
		return false
	}
	switch channel.Type {
	case discordgo.ChannelTypeGuildText,
		discordgo.ChannelTypeGuildNews,
		discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
		discordgo.ChannelTypeGuildNewsThread:
		return true
	default:
		return false
	}
}

func toChannelRecord(channel *discordgo.Channel, raw string) store.ChannelRecord {
	record := store.ChannelRecord{
		ID:       channel.ID,
		GuildID:  channel.GuildID,
		ParentID: channel.ParentID,
		Kind:     channelKind(channel),
		Name:     channel.Name,
		Topic:    channel.Topic,
		Position: channel.Position,
		IsNSFW:   channel.NSFW,
		RawJSON:  raw,
	}
	if channel.ThreadMetadata != nil {
		record.IsArchived = channel.ThreadMetadata.Archived
		record.IsLocked = channel.ThreadMetadata.Locked
		record.ArchiveTimestamp = channel.ThreadMetadata.ArchiveTimestamp.Format(time.RFC3339Nano)
		record.ThreadParentID = channel.ParentID
	}
	if channel.Type == discordgo.ChannelTypeGuildPrivateThread {
		record.IsPrivateThread = true
	}
	return record
}

func channelKind(channel *discordgo.Channel) string {
	switch channel.Type {
	case discordgo.ChannelTypeGuildCategory:
		return "category"
	case discordgo.ChannelTypeGuildText:
		return "text"
	case discordgo.ChannelTypeGuildNews:
		return "announcement"
	case discordgo.ChannelTypeGuildForum:
		return "forum"
	case discordgo.ChannelTypeGuildPublicThread:
		return "thread_public"
	case discordgo.ChannelTypeGuildPrivateThread:
		return "thread_private"
	case discordgo.ChannelTypeGuildNewsThread:
		return "thread_announcement"
	case discordgo.ChannelTypeGuildVoice:
		return "voice"
	default:
		return fmt.Sprintf("type_%d", channel.Type)
	}
}

func channelTypeFromKind(kind string) discordgo.ChannelType {
	switch kind {
	case "category":
		return discordgo.ChannelTypeGuildCategory
	case "text":
		return discordgo.ChannelTypeGuildText
	case "announcement":
		return discordgo.ChannelTypeGuildNews
	case "forum":
		return discordgo.ChannelTypeGuildForum
	case "thread_public":
		return discordgo.ChannelTypeGuildPublicThread
	case "thread_private":
		return discordgo.ChannelTypeGuildPrivateThread
	case "thread_announcement":
		return discordgo.ChannelTypeGuildNewsThread
	case "voice":
		return discordgo.ChannelTypeGuildVoice
	default:
		return discordgo.ChannelTypeGuildText
	}
}
