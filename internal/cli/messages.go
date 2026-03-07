package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/steipete/discrawl/internal/store"
)

const defaultMessageLimit = 200

func (r *runtime) runMessages(args []string) error {
	fs := flag.NewFlagSet("messages", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	channel := fs.String("channel", "", "")
	author := fs.String("author", "", "")
	days := fs.Int("days", 0, "")
	since := fs.String("since", "", "")
	before := fs.String("before", "", "")
	limit := fs.Int("limit", defaultMessageLimit, "")
	all := fs.Bool("all", false, "")
	includeEmpty := fs.Bool("include-empty", false, "")
	guildsFlag := fs.String("guilds", "", "")
	guildFlag := fs.String("guild", "", "")
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 0 {
		return usageErr(fmt.Errorf("messages takes flags only"))
	}
	if *days < 0 {
		return usageErr(fmt.Errorf("--days must be >= 0"))
	}
	if *days > 0 && strings.TrimSpace(*since) != "" {
		return usageErr(fmt.Errorf("use either --days or --since"))
	}
	if *limit < 0 {
		return usageErr(fmt.Errorf("--limit must be >= 0"))
	}

	var sinceTime time.Time
	var beforeTime time.Time
	var err error
	if *days > 0 {
		now := time.Now().UTC()
		if r.now != nil {
			now = r.now().UTC()
		}
		sinceTime = now.Add(-time.Duration(*days) * 24 * time.Hour)
	}
	if strings.TrimSpace(*since) != "" {
		sinceTime, err = time.Parse(time.RFC3339, *since)
		if err != nil {
			return usageErr(fmt.Errorf("invalid --since: %w", err))
		}
	}
	if strings.TrimSpace(*before) != "" {
		beforeTime, err = time.Parse(time.RFC3339, *before)
		if err != nil {
			return usageErr(fmt.Errorf("invalid --before: %w", err))
		}
	}

	guildIDs := r.resolveSearchGuilds(*guildFlag, *guildsFlag)
	if strings.TrimSpace(*channel) == "" && strings.TrimSpace(*author) == "" && sinceTime.IsZero() && beforeTime.IsZero() && len(guildIDs) == 0 {
		return usageErr(fmt.Errorf("messages needs at least one filter"))
	}
	if *all {
		*limit = 0
	}

	rows, err := r.store.ListMessages(r.ctx, store.MessageListOptions{
		GuildIDs:     guildIDs,
		Channel:      *channel,
		Author:       *author,
		Since:        sinceTime,
		Before:       beforeTime,
		Limit:        *limit,
		IncludeEmpty: *includeEmpty,
	})
	if err != nil {
		return err
	}
	return r.print(rows)
}
