# discrawl

`discrawl` mirrors Discord guild data into local SQLite so you can search, inspect, and query server history without depending on Discord search.

It is a bot-token crawler. No user-token hacks. Data stays local.

## What It Does

- discovers every guild the configured bot can access
- syncs channels, threads, members, and message history into SQLite
- maintains FTS5 search indexes for fast local text search
- tails Gateway events for live updates, with periodic repair syncs
- exposes read-only SQL for ad hoc analysis
- keeps schema multi-guild ready while preserving a simple single-guild default UX

Search defaults to all guilds. `sync` and `tail` default to the configured default guild when one exists, otherwise they fan out to all discovered guilds.

## Requirements

- Go `1.26+`
- a Discord bot token the bot can use to read the target guilds
- bot permissions for the channels you want archived

Token resolution:

1. OpenClaw config, if `discord.token_source` is not `env`
2. `DISCORD_BOT_TOKEN` or the configured `discord.token_env`

Default runtime paths:

- config: `~/.discrawl/config.toml`
- database: `~/.discrawl/discrawl.db`
- cache: `~/.discrawl/cache/`
- logs: `~/.discrawl/logs/`

## Install

Build from source:

```bash
git clone https://github.com/steipete/discrawl.git
cd discrawl
go build -o bin/discrawl ./cmd/discrawl
./bin/discrawl --version
```

## Quick Start

Reuse an existing OpenClaw Discord bot config:

```bash
bin/discrawl init --from-openclaw ~/.openclaw/openclaw.json
bin/discrawl doctor
bin/discrawl sync --full
bin/discrawl search "panic: nil pointer"
bin/discrawl tail
```

Env-only setup:

```bash
export DISCORD_BOT_TOKEN="..."
bin/discrawl init
bin/discrawl sync --full
```

`init` discovers accessible guilds and writes `~/.discrawl/config.toml`. If exactly one guild is available, that guild becomes the default automatically.

## Commands

### `init`

Creates the local config and discovers accessible guilds.

```bash
bin/discrawl init
bin/discrawl init --from-openclaw ~/.openclaw/openclaw.json
bin/discrawl init --guild 123456789012345678
bin/discrawl init --db ~/data/discrawl.db
```

### `sync`

Backfills guild state into SQLite.

```bash
bin/discrawl sync --full
bin/discrawl sync --guild 123456789012345678
bin/discrawl sync --guilds 123,456 --concurrency 8
bin/discrawl sync --channels 111,222 --since 2026-03-01T00:00:00Z
```

`sync` already uses parallel channel workers. `--concurrency` overrides the default, and the default is auto-sized from `GOMAXPROCS` with a floor of `8` and a cap of `32`.

### `tail`

Runs the live Gateway tail and periodic repair loop.

```bash
bin/discrawl tail
bin/discrawl tail --guild 123456789012345678
bin/discrawl tail --repair-every 30m
```

### `search`

Runs FTS search over archived messages.

```bash
bin/discrawl search "panic: nil pointer"
bin/discrawl search --guild 123456789012345678 "payment failed"
bin/discrawl search --channel billing --author steipete --limit 50 "invoice"
bin/discrawl --json search "websocket closed"
```

### `sql`

Runs read-only SQL against the local database.

```bash
bin/discrawl sql 'select count(*) as messages from messages'
echo 'select guild_id, count(*) from messages group by guild_id' | bin/discrawl sql -
```

### `members`

```bash
bin/discrawl members list
bin/discrawl members show 123456789012345678
bin/discrawl members search "peter"
```

### `channels`

```bash
bin/discrawl channels list
bin/discrawl channels show 123456789012345678
```

### `status`

Shows local archive status.

```bash
bin/discrawl status
```

### `doctor`

Checks config, auth, DB, and FTS wiring.

```bash
bin/discrawl doctor
```

## Configuration

`init` writes a complete config, so most users should not hand-edit anything initially.

Typical config shape:

```toml
version = 1
default_guild_id = ""
guild_ids = []
db_path = "~/.discrawl/discrawl.db"
cache_dir = "~/.discrawl/cache"
log_dir = "~/.discrawl/logs"

[discord]
token_source = "openclaw"
openclaw_config = "~/.openclaw/openclaw.json"
account = "default"
token_env = "DISCORD_BOT_TOKEN"

[sync]
concurrency = 16
repair_every = "6h"
full_history = true

[search]
default_mode = "fts"

[search.embeddings]
enabled = false
provider = "openai"
model = "text-embedding-3-small"
api_key_env = "OPENAI_API_KEY"
batch_size = 64
```

The value above is an example. `init` writes an auto-sized default based on the host: `min(32, max(8, GOMAXPROCS*2))`.

Config override rules:

- `--config` beats everything
- `DISCRAWL_CONFIG` overrides the default config path
- `discord.token_source = "env"` forces env-only token lookup

## Embeddings

Embeddings are optional. FTS is the default search path and the primary verification target.

If enabled, embeddings are intended to enrich recall in background batches, not block the hot sync path.

```bash
export OPENAI_API_KEY="..."
bin/discrawl init --with-embeddings
bin/discrawl sync --with-embeddings
```

## Data Stored Locally

- guild metadata
- channels and threads in one table
- current member snapshot
- canonical message rows
- append-only message event records
- FTS index rows
- optional embedding backlog metadata

Attachment binaries are not stored in SQLite.

## Security

- do not commit bot tokens or API keys
- default config lives in your home directory, not inside the repo
- CI runs secret scanning with `gitleaks`
- `doctor` reports token source, not token contents

## Development

Local gate:

```bash
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.1 run
go test ./... -coverprofile=/tmp/discrawl.cover
go tool cover -func=/tmp/discrawl.cover | tail -n 1
go build ./cmd/discrawl
```

Target coverage is `>= 80%`.

CI runs:

- `golangci-lint`
- `go test` with coverage threshold enforcement
- `go build ./cmd/discrawl`
- `gitleaks` against git history and the working tree

## Notes

- the schema is multi-guild ready even when the common UX stays single-guild simple
- threads are stored as channels because that matches the Discord model
- archived threads are part of the sync surface
- live sync is resumable; large guilds still take time because Discord rate limits history backfill

## License

MIT. See [LICENSE](LICENSE).
