package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

const (
	defaultHTTPPort        = "8080"
	credentialsEnvFileName = "cred.env"
	pathsEnvFileName       = "paths.env"
)

type AppConfig struct {
	Port               string
	ClientID           string
	ClientSecret       string
	RedirectURI        string
	DatabaseURL        string
	DataDir            string
	DetailsCachePath   string
	CORSAllowedOrigins []string
	LogLevel           string
	LogFormat          string
}

func loadConfig() AppConfig {
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
		Port:               firstNonEmpty(get("PORT"), defaultHTTPPort),
		ClientID:           get("MAL_CLIENT_ID"),
		ClientSecret:       get("MAL_CLIENT_SECRET"),
		RedirectURI:        get("MAL_REDIRECT_URI"),
		DatabaseURL:        firstNonEmpty(get("DATABASE_URL"), get("MAL_DATABASE_URL")),
		DataDir:            dataDir,
		CORSAllowedOrigins: parseCommaSeparatedValues(get("CORS_ALLOWED_ORIGINS")),
		LogLevel:           get("LOG_LEVEL"),
		LogFormat:          get("LOG_FORMAT"),
	}

	cfg.DetailsCachePath = resolveAppPath(cfg.DataDir, detailsCacheName)

	return cfg
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
