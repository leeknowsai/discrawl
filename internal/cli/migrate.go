package cli

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/steipete/discrawl/internal/config"
	"github.com/steipete/discrawl/internal/store"
)

func (r *runtime) runMigrateDB(args []string) error {
	fs := flag.NewFlagSet("migrate-db", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "preview migration without writing")
	dataDir := fs.String("data-dir", "", "target directory for per-guild DBs (default: ~/.discrawl)")
	sourceDB := fs.String("source", "", "source discrawl.db path (default: from config)")
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}

	cfg, err := config.Load(r.configPath)
	if err != nil {
		return configErr(err)
	}
	r.cfg = cfg

	src := *sourceDB
	if src == "" {
		src, err = config.ExpandPath(cfg.DBPath)
		if err != nil {
			return configErr(err)
		}
	}

	target := *dataDir
	if target == "" {
		target = filepath.Dir(src)
	}

	r.logger.Info("starting db migration",
		"source", src,
		"target", target,
		"dry_run", *dryRun,
	)

	result, err := store.MigrateSplitDB(r.ctx, store.MigrateOptions{
		SourceDB: src,
		DataDir:  target,
		Logger:   r.logger,
		DryRun:   *dryRun,
	})
	if err != nil {
		return dbErr(err)
	}

	if *dryRun {
		fmt.Fprintf(r.stdout, "Dry run: would migrate %d guild(s)\n", result.GuildCount)
		for _, gr := range result.GuildResults {
			fmt.Fprintf(r.stdout, "  - %s (%s)\n", gr.GuildID, gr.GuildName)
		}
		return nil
	}

	fmt.Fprintf(r.stdout, "Migration complete: %d guild(s)\n", result.GuildCount)
	for _, gr := range result.GuildResults {
		fmt.Fprintf(r.stdout, "  %s (%s): %d messages, %d members, %d channels\n",
			gr.GuildID, gr.GuildName, gr.Messages, gr.Members, gr.Channels)
	}
	return nil
}
