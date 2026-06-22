package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"test/internal/adapters/filecache"
	"test/internal/ports"

	"github.com/joho/godotenv"
)

const (
	DefaultHTTPPort        = "8080"
	credentialsEnvFileName = "cred.env"
	pathsEnvFileName       = "paths.env"

	// Lazy-worker defaults. The worker is bounded per cycle by the batch size to
	// stay within MAL rate limits; the interval paces the cycles; the TTL decides
	// when a resolved entry's details (and mal_score) count as stale.
	DefaultLazyWorkerInterval  = time.Minute
	DefaultLazyWorkerBatchSize = 200
	DefaultLazyWorkerTTL       = ports.DetailsCacheTTL
)

type AppConfig struct {
	Port                      string
	ClientID                  string
	ClientSecret              string
	RedirectURI               string
	FrontendURL               string
	SessionSecret             string
	DatabaseURL               string
	DataDir                   string
	DetailsCachePath          string
	HydrationFailureCachePath string
	CORSAllowedOrigins        []string
	LogLevel                  string
	LogFormat                 string

	LazyWorkerInterval  time.Duration
	LazyWorkerBatchSize int
	LazyWorkerTTL       time.Duration
}

func LoadConfig() AppConfig {
	values := make(map[string]string)

	for _, path := range []string{credentialsEnvFileName, pathsEnvFileName} {
		loaded, err := godotenv.Read(path)
		if err != nil {
			if !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "warning: cannot load %s: %v\n", path, err)
			}
			continue
		}

		for key, value := range loaded {
			values[key] = value
		}
	}

	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		values[key] = value
	}

	get := func(key string) string {
		return strings.TrimSpace(values[key])
	}

	dataDir := get("MAL_DATA_DIR")

	cfg := AppConfig{
		Port:               firstNonEmpty(get("PORT"), DefaultHTTPPort),
		ClientID:           get("MAL_CLIENT_ID"),
		ClientSecret:       get("MAL_CLIENT_SECRET"),
		RedirectURI:        get("MAL_REDIRECT_URI"),
		FrontendURL:        get("MAL_FRONTEND_URL"),
		SessionSecret:      get("MAL_SESSION_SECRET"),
		DatabaseURL:        firstNonEmpty(get("DATABASE_URL"), get("MAL_DATABASE_URL")),
		DataDir:            dataDir,
		CORSAllowedOrigins: parseCommaSeparatedValues(get("CORS_ALLOWED_ORIGINS")),
		LogLevel:           get("LOG_LEVEL"),
		LogFormat:          get("LOG_FORMAT"),

		LazyWorkerInterval:  parseDurationOr(get("LAZY_WORKER_INTERVAL"), DefaultLazyWorkerInterval),
		LazyWorkerBatchSize: parsePositiveIntOr(get("LAZY_WORKER_BATCH_SIZE"), DefaultLazyWorkerBatchSize),
		LazyWorkerTTL:       parseDurationOr(get("LAZY_WORKER_TTL"), DefaultLazyWorkerTTL),
	}

	cfg.DetailsCachePath = resolveAppPath(cfg.DataDir, filecache.DetailsCacheName)
	cfg.HydrationFailureCachePath = resolveAppPath(cfg.DataDir, filecache.HydrationFailureCacheName)

	return cfg
}

func parseDurationOr(raw string, fallback time.Duration) time.Duration {
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		fmt.Fprintf(os.Stderr, "warning: invalid duration %q, using %s\n", raw, fallback)
		return fallback
	}
	return value
}

func parsePositiveIntOr(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		fmt.Fprintf(os.Stderr, "warning: invalid positive integer %q, using %d\n", raw, fallback)
		return fallback
	}
	return value
}

func parseCommaSeparatedValues(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))

	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}

	return values
}

func resolveAppPath(baseDir, name string) string {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return name
	}
	return filepath.Join(baseDir, name)
}
