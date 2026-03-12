package cli

import (
	"flag"
	"fmt"
	"time"

	"github.com/steipete/discrawl/internal/config"
	"github.com/steipete/discrawl/internal/discord"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/syncer"
	"github.com/steipete/discrawl/internal/web"
)

func (r *runtime) runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(r.stderr)
	port := fs.Int("port", 0, "HTTP listen port (default: from config, fallback 8080)")
	host := fs.String("host", "", "HTTP listen host (default: from config, fallback localhost)")
	tail := fs.Bool("tail", false, "Start syncer in background for live updates")
	guildsFlag := fs.String("guilds", "", "Comma-separated guild IDs to tail (default: all)")
	guildFlag := fs.String("guild", "", "Single guild ID to tail")
	repairEvery := fs.Duration("repair-every", 0, "Run full repair sync every N duration (e.g. 1h)")
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}

	cfg, err := config.Load(r.configPath)
	if err != nil {
		return configErr(err)
	}
	if err := config.EnsureRuntimeDirs(cfg); err != nil {
		return configErr(err)
	}

	// Generate and save session secret on first init if missing.
	if err := config.EnsureSessionSecret(r.configPath, &cfg); err != nil {
		return configErr(fmt.Errorf("session secret: %w", err))
	}

	dataDir, err := config.ExpandPath(cfg.EffectiveDataDir())
	if err != nil {
		return configErr(fmt.Errorf("data dir: %w", err))
	}

	registry, err := store.NewRegistry(r.ctx, store.RegistryConfig{
		DataDir: dataDir,
	})
	if err != nil {
		return dbErr(fmt.Errorf("open registry: %w", err))
	}
	defer func() { _ = registry.Close() }()

	listenHost := cfg.Web.Host
	if *host != "" {
		listenHost = *host
	}
	listenPort := cfg.Web.Port
	if *port != 0 {
		listenPort = *port
	}

	srv := web.NewServer(cfg, r.configPath, registry, r.logger)

	// If --tail is set, start syncer in background.
	if *tail {
		token, err := config.ResolveDiscordToken(cfg)
		if err != nil {
			return authErr(err)
		}
		var client *discord.Client
		if token.IsUser {
			client, err = discord.NewUser(token.Token)
		} else {
			client, err = discord.New(token.Token)
		}
		if err != nil {
			return authErr(err)
		}
		defer func() { _ = client.Close() }()

		// Create syncer with first guild store (or default).
		// For multi-tenant, we'll need to iterate or use primary guild.
		// For now, use the default guild from config.
		guildID := cfg.EffectiveDefaultGuildID()
		guildStore, err := registry.Get(r.ctx, guildID)
		if err != nil {
			return dbErr(fmt.Errorf("open guild store for tail: %w", err))
		}

		sync := syncer.New(client, guildStore, r.logger)
		sync.SetAttachmentTextEnabled(cfg.AttachmentTextEnabled())
		srv.SetSyncer(sync)

		// Resolve guilds to tail.
		guildIDs := r.resolveSyncGuilds(*guildFlag, *guildsFlag)
		if len(guildIDs) == 0 {
			guildIDs = []string{guildID}
		}
		repairInterval := *repairEvery
		if repairInterval == 0 && cfg.Sync.RepairEvery != "" {
			repairInterval, _ = time.ParseDuration(cfg.Sync.RepairEvery)
		}

		if err := srv.StartTail(r.ctx, guildIDs, repairInterval); err != nil {
			return fmt.Errorf("start tail: %w", err)
		}
		r.logger.Info("serve with live updates enabled", "guilds", guildIDs)
	}

	return srv.ListenAndServe(r.ctx, listenHost, listenPort)
}
