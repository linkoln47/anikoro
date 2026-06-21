package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"test/internal/app"
	"test/internal/ports"
)

// refresh-catalog re-hydrates the oldest catalog entries so their community
// score (anime_catalog.mal_score) and other details do not drift for anime that
// no recent user sync has touched. Run it on a schedule (system cron, k8s
// CronJob, ...). It is bounded by -limit per run to stay within MAL rate limits.
func main() {
	fs := flag.NewFlagSet("refresh-catalog", flag.ExitOnError)
	olderThan := fs.Duration(
		"older-than",
		ports.DetailsCacheTTL,
		"re-hydrate catalog entries whose details are older than this; below the details cache TTL (168h) entries are still treated as fresh and skipped",
	)
	limit := fs.Int("limit", 200, "maximum number of catalog entries to refresh in this run")
	_ = fs.Parse(os.Args[1:])

	application := app.NewApp()
	defer func() { _ = application.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	refreshed, err := application.RunCatalogRefresh(ctx, *olderThan, *limit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("ok: refreshed=%d older_than=%s limit=%d\n", refreshed, olderThan.String(), *limit)
}
