package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"test/internal/adapters/postgres"
	"test/internal/app"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cfg := app.LoadConfig()
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "up":
		if err := postgres.MigrateUp(cfg.DatabaseURL); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		version, dirty, _ := postgres.MigrateVersion(cfg.DatabaseURL)
		fmt.Printf("ok: version=%d dirty=%v\n", version, dirty)

	case "down":
		fs := flag.NewFlagSet("down", flag.ExitOnError)
		steps := fs.Int("steps", 1, "number of steps to roll back")
		_ = fs.Parse(args)
		if err := postgres.MigrateDown(cfg.DatabaseURL, *steps); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		version, dirty, _ := postgres.MigrateVersion(cfg.DatabaseURL)
		fmt.Printf("ok: version=%d dirty=%v\n", version, dirty)

	case "version":
		version, dirty, err := postgres.MigrateVersion(cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("version=%d dirty=%v\n", version, dirty)

	case "force":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "force requires a version argument")
			os.Exit(2)
		}
		v, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid version:", err)
			os.Exit(2)
		}
		if err := postgres.MigrateForce(cfg.DatabaseURL, v); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("forced version=%d\n", v)

	case "goto":
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "goto requires a version argument")
			os.Exit(2)
		}
		v, err := strconv.ParseUint(args[0], 10, 32)
		if err != nil {
			fmt.Fprintln(os.Stderr, "invalid version:", err)
			os.Exit(2)
		}
		if err := postgres.MigrateGoto(cfg.DatabaseURL, uint(v)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		version, dirty, _ := postgres.MigrateVersion(cfg.DatabaseURL)
		fmt.Printf("ok: version=%d dirty=%v\n", version, dirty)

	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: migrate <command> [args]

commands:
  up                apply all pending migrations
  down -steps N     roll back N migrations (default 1)
  version           print current version and dirty flag
  force V           mark version V applied without running it
  goto V            migrate up or down to exactly version V

reads DATABASE_URL (or MAL_DATABASE_URL) from env or env files
(cred.env, paths.env) in the current working directory.`)
}
