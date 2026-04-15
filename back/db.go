package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

const (
	dbFileName      = "mal.db"
	seriesTableName = "series_table"
	movieTableName  = "movie_table"
)

type groupedView struct {
	ID                 int
	GroupKey           string
	DisplayTitle       string
	MergedTitles       int
	AvgScore           float64
	WatchedEpisodesSum int
}

func (a *App) listAnime() ([]AnimeItem, error) {
	anime := make([]AnimeItem, 0)

	series, err := listAnimeByType(a.DB, seriesTableName, "series")
	if err != nil {
		return nil, err
	}
	anime = append(anime, series...)

	movies, err := listAnimeByType(a.DB, movieTableName, "movie")
	if err != nil {
		return nil, err
	}
	anime = append(anime, movies...)

	return anime, nil
}

func listAnimeByType(db *sql.DB, table, animeType string) ([]AnimeItem, error) {
	rows, err := db.Query(`
		SELECT id, display_title, merged_titles, avg_score, watched_episodes_sum, synced_at
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

func (a *App) getStats() (StatsResponse, error) {
	var stats StatsResponse

	if err := a.DB.QueryRow("SELECT COUNT(*) FROM " + seriesTableName).Scan(&stats.SeriesCount); err != nil {
		return StatsResponse{}, fmt.Errorf("count series: %w", err)
	}

	if err := a.DB.QueryRow("SELECT COUNT(*) FROM " + movieTableName).Scan(&stats.MoviesCount); err != nil {
		return StatsResponse{}, fmt.Errorf("count movies: %w", err)
	}

	stats.TotalCount = stats.SeriesCount + stats.MoviesCount
	return stats, nil
}

func openDB(cfg AppConfig) (*sql.DB, error) {
	info, err := os.Stat(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("stat sqlite database: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("sqlite database path points to a directory: %s", cfg.DBPath)
	}

	db, err := sql.Open("sqlite", cfg.DBPath)
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

	if err := validateDBContract(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("validate sqlite database contract: %w", err)
	}

	return db, nil
}

func validateDBContract(db *sql.DB) error {
	for _, table := range []string{seriesTableName, movieTableName} {
		if err := validateGroupedTable(db, table); err != nil {
			return err
		}
	}

	return nil
}

func validateGroupedTable(db *sql.DB, table string) error {
	rows, err := db.Query(`
		SELECT id, group_key, display_title, merged_titles, avg_score, watched_episodes_sum, synced_at
		FROM ` + table + `
		LIMIT 0
	`)
	if err != nil {
		return fmt.Errorf("validate table %s: %w", table, err)
	}

	return rows.Close()
}

func (a *App) saveGroupedLists(seriesGroups, movieGroups []groupedView) error {
	tx, err := a.DB.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := a.replaceGroups(tx, seriesTableName, seriesGroups); err != nil {
		return err
	}
	if err := a.replaceGroups(tx, movieTableName, movieGroups); err != nil {
		return err
	}

	return tx.Commit()
}

func (a *App) replaceGroups(tx *sql.Tx, table string, groups []groupedView) error {
	a.logInfo("db", "rewriting DB table", "table", table, "rows", len(groups))

	if _, err := tx.Exec("DELETE FROM " + table); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO ` + table + ` (
			id,
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
			g.ID,
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
