package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	dbFileName      = "mal.db"
	seriesTableName = "series_table"
	movieTableName  = "movie_table"
)

const createSeriesTableSQL = `
CREATE TABLE IF NOT EXISTS ` + seriesTableName + ` (
  id INTEGER PRIMARY KEY,
  canonical_mal_id INTEGER NOT NULL,
  group_key TEXT NOT NULL,
  display_title TEXT NOT NULL,
  merged_titles INTEGER NOT NULL,
  avg_score REAL NOT NULL,
  watched_episodes_sum INTEGER NOT NULL,
  synced_at TEXT NOT NULL
);
`

const createMoviesTableSQL = `
CREATE TABLE IF NOT EXISTS ` + movieTableName + ` (
  id INTEGER PRIMARY KEY,
  canonical_mal_id INTEGER NOT NULL,
  group_key TEXT NOT NULL,
  display_title TEXT NOT NULL,
  merged_titles INTEGER NOT NULL,
  avg_score REAL NOT NULL,
  watched_episodes_sum INTEGER NOT NULL,
  synced_at TEXT NOT NULL
);
`

type groupedView struct {
	CanonicalMALID     int
	GroupKey           string
	DisplayTitle       string
	MergedTitles       int
	AvgScore           float64
	WatchedEpisodesSum int
}

func listAnime(db *sql.DB) ([]AnimeItem, error) {
	anime := make([]AnimeItem, 0)

	series, err := listAnimeByType(db, seriesTableName, "series")
	if err != nil {
		return nil, err
	}
	anime = append(anime, series...)

	movies, err := listAnimeByType(db, movieTableName, "movie")
	if err != nil {
		return nil, err
	}
	anime = append(anime, movies...)

	return anime, nil
}

func listAnimeByType(db *sql.DB, table, animeType string) ([]AnimeItem, error) {
	rows, err := db.Query(`
		SELECT canonical_mal_id, display_title, merged_titles, avg_score, watched_episodes_sum, synced_at
		FROM ` + table + `
		ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", table, err)
	}
	defer rows.Close()

	var anime []AnimeItem
	for rows.Next() {
		var item AnimeItem
		if err := rows.Scan(
			&item.ID,
			&item.DisplayTitle,
			&item.MergedTitles,
			&item.AvgScore,
			&item.WatchedEpisodesSum,
			&item.SyncedAt,
		); err != nil {
			return nil, fmt.Errorf("scan %s row: %w", table, err)
		}
		item.Type = animeType
		anime = append(anime, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s rows: %w", table, err)
	}

	return anime, nil
}

func getStats(db *sql.DB) (StatsResponse, error) {
	var stats StatsResponse

	if err := db.QueryRow("SELECT COUNT(*) FROM " + seriesTableName).Scan(&stats.SeriesCount); err != nil {
		return StatsResponse{}, fmt.Errorf("count series: %w", err)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM " + movieTableName).Scan(&stats.MoviesCount); err != nil {
		return StatsResponse{}, fmt.Errorf("count movies: %w", err)
	}

	stats.TotalCount = stats.SeriesCount + stats.MoviesCount
	return stats, nil
}

func migrateDB(db *sql.DB) error {
	_, err := db.Exec(createSeriesTableSQL)
	if err != nil {
		return err
	}
	_, err = db.Exec(createMoviesTableSQL)
	if err != nil {
		return err
	}
	if err := ensureGroupedTableSchema(db, seriesTableName); err != nil {
		return err
	}
	if err := ensureGroupedTableSchema(db, movieTableName); err != nil {
		return err
	}
	return nil
}

func ensureGroupedTableSchema(db *sql.DB, table string) error {
	columns, err := loadTableColumns(db, table)
	if err != nil {
		return err
	}

	if !columns["canonical_mal_id"] {
		if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN canonical_mal_id INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add canonical_mal_id to %s: %w", table, err)
		}
	}

	if !columns["group_key"] {
		if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN group_key TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add group_key to %s: %w", table, err)
		}
	}

	if _, err := db.Exec(`UPDATE ` + table + ` SET canonical_mal_id = id WHERE canonical_mal_id = 0`); err != nil {
		return fmt.Errorf("backfill canonical_mal_id for %s: %w", table, err)
	}

	if _, err := db.Exec(`UPDATE ` + table + ` SET group_key = 'legacy:' || id WHERE group_key = ''`); err != nil {
		return fmt.Errorf("backfill group_key for %s: %w", table, err)
	}

	indexName := table + `_group_key_idx`
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ` + indexName + ` ON ` + table + ` (group_key)`); err != nil {
		return fmt.Errorf("create group_key index for %s: %w", table, err)
	}

	return nil
}

func loadTableColumns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, fmt.Errorf("load columns for %s: %w", table, err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return nil, fmt.Errorf("scan column info for %s: %w", table, err)
		}
		columns[name] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns for %s: %w", table, err)
	}

	return columns, nil
}

func openDB() (*sql.DB, error) {
	dbPath := strings.TrimSpace(os.Getenv("MAL_DB_PATH"))
	if dbPath == "" {
		dbPath = appFilePath(dbFileName)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	if err := migrateDB(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite database: %w", err)
	}

	return db, nil
}

func saveGroupedLists(db *sql.DB, seriesGroups, movieGroups []groupedView) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := replaceGroups(tx, seriesTableName, seriesGroups); err != nil {
		return err
	}
	if err := replaceGroups(tx, movieTableName, movieGroups); err != nil {
		return err
	}

	return tx.Commit()
}

func replaceGroups(tx *sql.Tx, table string, groups []groupedView) error {
	oldSnapshot, err := loadGroupSnapshot(tx, table)
	if err != nil {
		return err
	}

	newSnapshot := renderGroupSnapshot(groups)
	if oldSnapshot == newSnapshot {
		logInfo("db", "database table unchanged", "table", table, "changes", 0)
		return nil
	}

	added, removed := countLineChanges(oldSnapshot, newSnapshot)
	logInfo("db", "database table updated", "table", table, "lines_added", added, "lines_removed", removed)

	if _, err := tx.Exec("DELETE FROM " + table); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO ` + table + ` (
			canonical_mal_id,
			group_key,
			display_title,
			merged_titles,
			avg_score,
			watched_episodes_sum,
			synced_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	syncedAt := time.Now().UTC().Format(time.RFC3339)
	for _, g := range groups {
		if _, err := stmt.Exec(
			g.CanonicalMALID,
			g.GroupKey,
			g.DisplayTitle,
			g.MergedTitles,
			g.AvgScore,
			g.WatchedEpisodesSum,
			syncedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func loadGroupSnapshot(tx *sql.Tx, table string) (string, error) {
	rows, err := tx.Query(`
		SELECT
			canonical_mal_id,
			group_key,
			display_title,
			merged_titles,
			avg_score,
			watched_episodes_sum
		FROM ` + table + `
		ORDER BY id
	`)
	if err != nil {
		return "", fmt.Errorf("load snapshot for %s: %w", table, err)
	}
	defer rows.Close()

	var groups []groupedView
	for rows.Next() {
		var group groupedView
		if err := rows.Scan(
			&group.CanonicalMALID,
			&group.GroupKey,
			&group.DisplayTitle,
			&group.MergedTitles,
			&group.AvgScore,
			&group.WatchedEpisodesSum,
		); err != nil {
			return "", fmt.Errorf("scan snapshot row for %s: %w", table, err)
		}
		groups = append(groups, group)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate snapshot rows for %s: %w", table, err)
	}

	return renderGroupSnapshot(groups), nil
}

func renderGroupSnapshot(groups []groupedView) string {
	var sb strings.Builder
	for _, g := range groups {
		sb.WriteString(fmt.Sprintf(
			"%d|%s|%s|%d|%.2f|%d\n",
			g.CanonicalMALID,
			g.GroupKey,
			g.DisplayTitle,
			g.MergedTitles,
			g.AvgScore,
			g.WatchedEpisodesSum,
		))
	}
	return sb.String()
}
