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
  display_title TEXT NOT NULL,
  merged_titles INTEGER NOT NULL,
  avg_score REAL NOT NULL,
  watched_episodes_sum INTEGER NOT NULL,
  synced_at TEXT NOT NULL
);
`

func migrateDB(db *sql.DB) error {
	_, err := db.Exec(createSeriesTableSQL)
	if err != nil {
		return err
	}
	_, err = db.Exec(createMoviesTableSQL)
	if err != nil {
		return err
	}
	return nil
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
		fmt.Printf("Database table %s exists, no changes (0)\n", table)
		return nil
	}

	added, removed := countLineChanges(oldSnapshot, newSnapshot)
	fmt.Printf("Database table %s updated, overwriting with changes: +%d / -%d\n", table, added, removed)

	if _, err := tx.Exec("DELETE FROM " + table); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO ` + table + ` (
			id,
			display_title,
			merged_titles,
			avg_score,
			watched_episodes_sum,
			synced_at
		) VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	syncedAt := time.Now().UTC().Format(time.RFC3339)
	for i, g := range groups {
		if _, err := stmt.Exec(
			i+1,
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
			"%s|%d|%.2f|%d\n",
			g.DisplayTitle,
			g.MergedTitles,
			g.AvgScore,
			g.WatchedEpisodesSum,
		))
	}
	return sb.String()
}
