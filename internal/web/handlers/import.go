package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/steipete/discrawl/internal/config"
	"github.com/steipete/discrawl/internal/discord"
	"github.com/steipete/discrawl/internal/store"
	"github.com/steipete/discrawl/internal/syncer"
	guildtmpl "github.com/steipete/discrawl/internal/web/templates/guild"
)

// HandleImportModal serves the import modal form.
func HandleImportModal() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = guildtmpl.ImportModal().Render(r.Context(), w)
	}
}

// HandleImportServer handles the POST request to import server data.
func HandleImportServer(
	configPath string,
	registry *store.Registry,
	logger *slog.Logger,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			respondError(w, "Invalid form data", http.StatusBadRequest)
			return
		}

		token := strings.TrimSpace(r.FormValue("token"))
		openclawPath := strings.TrimSpace(r.FormValue("openclaw_path"))

		if token == "" {
			respondError(w, "Discord bot token is required", http.StatusBadRequest)
			return
		}

		// Load existing config
		cfg, err := config.Load(configPath)
		if err != nil {
			logger.Error("failed to load config", "error", err)
			respondError(w, "Failed to load configuration", http.StatusInternalServerError)
			return
		}

		// Update OpenClaw path if provided
		if openclawPath != "" {
			cfg.Discord.OpenClawConfig = openclawPath
		}

		// Normalize token (remove "Bot " prefix if present)
		token = config.NormalizeBotToken(token)

		// Create Discord client
		client, err := discord.New(token)
		if err != nil {
			logger.Error("failed to create discord client", "error", err)
			respondError(w, "Invalid Discord token", http.StatusUnauthorized)
			return
		}
		defer func() { _ = client.Close() }()

		// Discover guilds using syncer
		syncerSvc := syncer.New(client, nil, logger)
		guilds, err := syncerSvc.DiscoverGuilds(r.Context())
		if err != nil {
			logger.Error("failed to discover guilds", "error", err)
			respondError(w, "Failed to discover guilds. Check token permissions.", http.StatusBadRequest)
			return
		}

		if len(guilds) == 0 {
			respondError(w, "No guilds found. Bot is not in any servers.", http.StatusBadRequest)
			return
		}

		// Update config with discovered guilds
		cfg.GuildIDs = make([]string, 0, len(guilds))
		for _, guild := range guilds {
			cfg.GuildIDs = append(cfg.GuildIDs, guild.ID)
		}

		// Set default guild if only one exists
		if cfg.DefaultGuildID == "" && len(cfg.GuildIDs) == 1 {
			cfg.DefaultGuildID = cfg.GuildIDs[0]
		}

		// Write updated config
		if err := config.Write(configPath, cfg); err != nil {
			logger.Error("failed to write config", "error", err)
			respondError(w, "Failed to save configuration", http.StatusInternalServerError)
			return
		}

		// Trigger sync for discovered guilds
		go func() {
			ctx := context.Background()
			opts := syncer.SyncOptions{
				Full:     false,
				GuildIDs: cfg.GuildIDs,
			}
			if _, err := syncerSvc.Sync(ctx, opts); err != nil {
				logger.Error("background sync failed", "error", err)
			}
		}()

		logger.Info("imported guilds successfully",
			"count", len(guilds),
			"guild_ids", cfg.GuildIDs,
		)

		// Redirect to guild selector
		w.Header().Set("HX-Redirect", "/app/guilds")
		w.WriteHeader(http.StatusOK)
	}
}

func respondError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	// Return HTML fragment that displays in the error div and makes it visible
	fmt.Fprintf(w, `<div class="bg-red-500/10 border border-red-500/50 rounded-lg p-3 text-red-400 text-sm">%s</div>`, message)
}
