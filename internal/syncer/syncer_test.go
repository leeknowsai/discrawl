package syncer

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/require"

	discordclient "github.com/steipete/discrawl/internal/discord"
	"github.com/steipete/discrawl/internal/store"
)

type fakeClient struct {
	guilds         []*discordgo.UserGuild
	guildByID      map[string]*discordgo.Guild
	channels       map[string][]*discordgo.Channel
	activeThreads  map[string][]*discordgo.Channel
	publicArchived map[string][]*discordgo.Channel
	privateArchive map[string][]*discordgo.Channel
	members        map[string][]*discordgo.Member
	messages       map[string][]*discordgo.Message
	tailCalls      int
	messageDelay   time.Duration
	mu             sync.Mutex
	inFlight       int
	maxInFlight    int
}

func (f *fakeClient) Self(context.Context) (*discordgo.User, error) {
	return &discordgo.User{ID: "bot"}, nil
}
func (f *fakeClient) Guilds(context.Context) ([]*discordgo.UserGuild, error) {
	return f.guilds, nil
}
func (f *fakeClient) Guild(_ context.Context, guildID string) (*discordgo.Guild, error) {
	return f.guildByID[guildID], nil
}
func (f *fakeClient) GuildChannels(_ context.Context, guildID string) ([]*discordgo.Channel, error) {
	return f.channels[guildID], nil
}
func (f *fakeClient) ThreadsActive(_ context.Context, channelID string) ([]*discordgo.Channel, error) {
	return f.activeThreads[channelID], nil
}
func (f *fakeClient) ThreadsArchived(_ context.Context, channelID string, private bool) ([]*discordgo.Channel, error) {
	if private {
		return f.privateArchive[channelID], nil
	}
	return f.publicArchived[channelID], nil
}
func (f *fakeClient) GuildMembers(_ context.Context, guildID string) ([]*discordgo.Member, error) {
	return f.members[guildID], nil
}
func (f *fakeClient) ChannelMessages(_ context.Context, channelID string, limit int, beforeID, afterID string) ([]*discordgo.Message, error) {
	if f.messageDelay > 0 {
		f.mu.Lock()
		f.inFlight++
		if f.inFlight > f.maxInFlight {
			f.maxInFlight = f.inFlight
		}
		f.mu.Unlock()
		time.Sleep(f.messageDelay)
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}
	all := f.messages[channelID]
	if afterID != "" {
		var filtered []*discordgo.Message
		for _, msg := range all {
			if msg.ID > afterID {
				filtered = append(filtered, msg)
			}
		}
		return filtered, nil
	}
	if beforeID == "" {
		if len(all) <= limit {
			return all, nil
		}
		return all[:limit], nil
	}
	return nil, nil
}
func (f *fakeClient) ChannelMessage(_ context.Context, channelID, messageID string) (*discordgo.Message, error) {
	for _, msg := range f.messages[channelID] {
		if msg.ID == messageID {
			return msg, nil
		}
	}
	return nil, nil
}
func (f *fakeClient) Tail(ctx context.Context, handler discordclient.EventHandler) error {
	f.tailCalls++
	msg := &discordgo.Message{
		ID:        "m3",
		GuildID:   "g1",
		ChannelID: "c1",
		Content:   "tail event",
		Timestamp: time.Now().UTC(),
		Author:    &discordgo.User{ID: "u1", Username: "peter"},
	}
	if err := handler.OnMessageCreate(ctx, msg); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func TestSyncFullAndIncremental(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "discrawl.db")
	s, err := store.Open(ctx, dbPath)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	client := &fakeClient{
		guilds: []*discordgo.UserGuild{{ID: "g1", Name: "Guild One"}},
		guildByID: map[string]*discordgo.Guild{
			"g1": {ID: "g1", Name: "Guild One"},
		},
		channels: map[string][]*discordgo.Channel{
			"g1": {
				{ID: "c1", GuildID: "g1", Name: "general", Type: discordgo.ChannelTypeGuildText},
			},
		},
		activeThreads: map[string][]*discordgo.Channel{
			"c1": {{ID: "t1", GuildID: "g1", ParentID: "c1", Name: "thread", Type: discordgo.ChannelTypeGuildPublicThread}},
		},
		members: map[string][]*discordgo.Member{
			"g1": {{
				GuildID: "g1",
				Nick:    "Peter",
				User:    &discordgo.User{ID: "u1", Username: "peter"},
			}},
		},
		messages: map[string][]*discordgo.Message{
			"c1": {{
				ID:        "100",
				GuildID:   "g1",
				ChannelID: "c1",
				Content:   "panic locked database",
				Timestamp: time.Now().UTC(),
				Author:    &discordgo.User{ID: "u1", Username: "peter"},
			}},
			"t1": {{
				ID:        "200",
				GuildID:   "g1",
				ChannelID: "t1",
				Content:   "thread post",
				Timestamp: time.Now().UTC(),
				Author:    &discordgo.User{ID: "u1", Username: "peter"},
			}},
		},
	}

	svc := New(client, s, nil)
	discovered, err := svc.DiscoverGuilds(ctx)
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	stats, err := svc.Sync(ctx, SyncOptions{Full: true})
	require.NoError(t, err)
	require.Equal(t, 1, stats.Guilds)
	require.Equal(t, 2, stats.Channels)
	require.Equal(t, 1, stats.Threads)
	require.Equal(t, 1, stats.Members)
	require.Equal(t, 2, stats.Messages)

	results, err := s.SearchMessages(ctx, store.SearchOptions{Query: "panic"})
	require.NoError(t, err)
	require.Len(t, results, 1)

	client.messages["c1"] = append(client.messages["c1"], &discordgo.Message{
		ID:        "101",
		GuildID:   "g1",
		ChannelID: "c1",
		Content:   "new message",
		Timestamp: time.Now().UTC(),
		Author:    &discordgo.User{ID: "u1", Username: "peter"},
	})
	stats, err = svc.Sync(ctx, SyncOptions{Full: false})
	require.NoError(t, err)
	require.Equal(t, 1, stats.Messages)
}

func TestSyncUsesConfiguredConcurrency(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "discrawl.db"))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	client := &fakeClient{
		guilds: []*discordgo.UserGuild{{ID: "g1", Name: "Guild"}},
		guildByID: map[string]*discordgo.Guild{
			"g1": {ID: "g1", Name: "Guild"},
		},
		channels: map[string][]*discordgo.Channel{
			"g1": {
				{ID: "c1", GuildID: "g1", Name: "one", Type: discordgo.ChannelTypeGuildText},
				{ID: "c2", GuildID: "g1", Name: "two", Type: discordgo.ChannelTypeGuildText},
			},
		},
		messages: map[string][]*discordgo.Message{
			"c1": {{ID: "10", GuildID: "g1", ChannelID: "c1", Content: "one", Timestamp: time.Now().UTC(), Author: &discordgo.User{ID: "u1", Username: "user"}}},
			"c2": {{ID: "20", GuildID: "g1", ChannelID: "c2", Content: "two", Timestamp: time.Now().UTC(), Author: &discordgo.User{ID: "u1", Username: "user"}}},
		},
		messageDelay: 40 * time.Millisecond,
	}

	svc := New(client, s, nil)
	stats, err := svc.Sync(ctx, SyncOptions{Full: true, Concurrency: 2})
	require.NoError(t, err)
	require.Equal(t, 2, stats.Messages)

	client.mu.Lock()
	maxInFlight := client.maxInFlight
	client.mu.Unlock()
	require.GreaterOrEqual(t, maxInFlight, 2)
}

func TestNormalizeMessageIncludesRichFields(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	message := &discordgo.Message{
		Content: "base",
		Attachments: []*discordgo.MessageAttachment{
			{Filename: "trace.txt"},
		},
		Embeds: []*discordgo.MessageEmbed{
			{Title: "title", Description: "desc"},
		},
		ReferencedMessage: &discordgo.Message{Content: "prior"},
		Poll: &discordgo.Poll{
			Question: discordgo.PollMedia{Text: "question"},
			Answers: []discordgo.PollAnswer{
				{Media: &discordgo.PollMedia{Text: "answer"}},
			},
		},
		Timestamp: now,
	}
	content := normalizeMessage(message)
	require.Contains(t, content, "base")
	require.Contains(t, content, "trace.txt")
	require.Contains(t, content, "title")
	require.Contains(t, content, "reply:prior")
	require.Contains(t, content, "question")
	require.Contains(t, content, "answer")
}

func TestTailHandlerWritesEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "discrawl.db"))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	handler := &tailHandler{store: s}
	msg := &discordgo.Message{
		ID:        "9",
		GuildID:   "g1",
		ChannelID: "c1",
		Content:   "tail event",
		Timestamp: time.Now().UTC(),
		Author:    &discordgo.User{ID: "u1", Username: "peter"},
	}
	require.NoError(t, handler.OnMessageCreate(ctx, msg))
	require.NoError(t, handler.OnMessageUpdate(ctx, msg))
	require.NoError(t, handler.OnMessageDelete(ctx, &discordgo.MessageDelete{Message: &discordgo.Message{
		ID:        "9",
		GuildID:   "g1",
		ChannelID: "c1",
	}}))
	require.NoError(t, handler.OnChannelUpsert(ctx, &discordgo.Channel{
		ID:      "c1",
		GuildID: "g1",
		Name:    "general",
		Type:    discordgo.ChannelTypeGuildText,
	}))
	require.NoError(t, handler.OnMemberUpsert(ctx, "g1", &discordgo.Member{
		GuildID: "g1",
		Nick:    "Peter",
		User:    &discordgo.User{ID: "u1", Username: "peter"},
	}))
	require.NoError(t, handler.OnMemberDelete(ctx, "g1", "u1"))

	status, err := s.Status(context.Background(), "db", "")
	require.NoError(t, err)
	require.Equal(t, 1, status.ChannelCount)
	require.Equal(t, 1, status.MessageCount)

	cursor, err := s.GetSyncState(context.Background(), "channel:c1:latest_message_id")
	require.NoError(t, err)
	require.Equal(t, "9", cursor)
}

func TestHelpers(t *testing.T) {
	t.Parallel()

	require.Equal(t, "101", maxSnowflake("100", "101"))
	require.Equal(t, "text", channelKind(&discordgo.Channel{Type: discordgo.ChannelTypeGuildText}))
	require.Equal(t, "thread_private", channelKind(&discordgo.Channel{Type: discordgo.ChannelTypeGuildPrivateThread}))
	require.Equal(t, "announcement", channelKind(&discordgo.Channel{Type: discordgo.ChannelTypeGuildNews}))
	require.Equal(t, "voice", channelKind(&discordgo.Channel{Type: discordgo.ChannelTypeGuildVoice}))
	require.True(t, isThreadParent(&discordgo.Channel{Type: discordgo.ChannelTypeGuildForum}))
	require.False(t, isThreadParent(&discordgo.Channel{Type: discordgo.ChannelTypeGuildVoice}))
	require.True(t, isMessageChannel(&discordgo.Channel{Type: discordgo.ChannelTypeGuildNewsThread}))
	require.False(t, isMessageChannel(&discordgo.Channel{Type: discordgo.ChannelTypeGuildCategory}))
	require.Len(t, selectGuilds([]*discordgo.UserGuild{{ID: "g1"}, {ID: "g2"}}, []string{"g2"}), 1)

	record := toChannelRecord(&discordgo.Channel{
		ID:       "t1",
		GuildID:  "g1",
		ParentID: "c1",
		Name:     "thread",
		Type:     discordgo.ChannelTypeGuildPrivateThread,
		ThreadMetadata: &discordgo.ThreadMetadata{
			Archived:         true,
			Locked:           true,
			ArchiveTimestamp: time.Now().UTC(),
		},
	}, `{}`)
	require.True(t, record.IsArchived)
	require.True(t, record.IsLocked)
	require.True(t, record.IsPrivateThread)

	handler := &tailHandler{guilds: makeGuildSet([]string{"g1"})}
	require.True(t, handler.allowGuild("g1"))
	require.False(t, handler.allowGuild("g2"))
	require.Equal(t, "Nick", displayName(&discordgo.Member{Nick: "Nick", User: &discordgo.User{Username: "user"}}))
	require.Equal(t, "Global", displayName(&discordgo.Member{User: &discordgo.User{GlobalName: "Global", Username: "user"}}))
}

func TestRunTail(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "discrawl.db"))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	client := &fakeClient{}
	svc := New(client, s, nil)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	err = svc.RunTail(ctx, nil, 0)
	require.True(t, err == nil || err == context.Canceled)

	status, err := s.Status(context.Background(), "db", "")
	require.NoError(t, err)
	require.Equal(t, 1, status.MessageCount)
	require.Equal(t, 1, client.tailCalls)
}

func TestRunTailWithRepairLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "discrawl.db"))
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	client := &fakeClient{
		guilds: []*discordgo.UserGuild{{ID: "g1", Name: "Guild"}},
		guildByID: map[string]*discordgo.Guild{
			"g1": {ID: "g1", Name: "Guild"},
		},
		channels: map[string][]*discordgo.Channel{
			"g1": {{ID: "c1", GuildID: "g1", Name: "general", Type: discordgo.ChannelTypeGuildText}},
		},
		messages: map[string][]*discordgo.Message{
			"c1": {{ID: "10", GuildID: "g1", ChannelID: "c1", Content: "repair", Timestamp: time.Now().UTC(), Author: &discordgo.User{ID: "u1", Username: "user"}}},
		},
	}
	svc := New(client, s, nil)
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()
	err = svc.RunTail(ctx, []string{"g1"}, 10*time.Millisecond)
	require.True(t, err == nil || err == context.Canceled)

	status, err := s.Status(context.Background(), "db", "")
	require.NoError(t, err)
	require.GreaterOrEqual(t, status.MessageCount, 1)
}
