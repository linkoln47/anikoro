package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"test/internal/app"
)

// lazy-worker is the cold-path catalog worker. It runs as a standalone
// long-lived container alongside the API server. Each cycle it hydrates catalog
// stubs the lightweight user sync leaves behind (fetching their details and
// franchise neighbours from MAL and rebuilding anime_franchises) and refreshes
// resolved entries whose details — and community mal_score — have gone stale.
//
// Defaults come from LAZY_WORKER_* env (see app.LoadConfig) and can be
// overridden by flags. Use -once for a single cycle (bootstrap or cron); a full
// first-time backfill over an existing catalog is `lazy-worker -once -batch=30000 -ttl=0`.
func main() {
	application := app.NewApp()
	cfg := application.Config

	fs := flag.NewFlagSet("lazy-worker", flag.ExitOnError)
	interval := fs.Duration("interval", cfg.LazyWorkerInterval, "delay between worker cycles")
	batch := fs.Int("batch", cfg.LazyWorkerBatchSize, "maximum catalog entries hydrated/refreshed per cycle, per pass")
	ttl := fs.Duration("ttl", cfg.LazyWorkerTTL, "re-fetch resolved entries whose details are older than this; below the details cache TTL (168h) entries are still treated as fresh and skipped")
	once := fs.Bool("once", false, "run a single cycle and exit (bootstrap or cron use)")
	_ = fs.Parse(os.Args[1:])

	defer func() { _ = application.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := application.RunLazyWorker(ctx, app.LazyWorkerConfig{
		Interval:  *interval,
		BatchSize: *batch,
		TTL:       *ttl,
		Once:      *once,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if *once {
		fmt.Println("ok: lazy worker cycle complete")
	}
}
