package main

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	animeEntriesTableName = "anime_entries"
	usersTableName        = "users"
	malTokensTable        = "mal_tokens"
	userScopeSetting      = "app.user_id"
	defaultDBTimeout      = 5 * time.Second
	defaultMaxOpenDB      = 10
	defaultMaxIdleDB      = 5
)

type User struct {
	ID       int64
	Username string
}

type groupedView struct {
	ID                 int
	GroupKey           string
	DisplayTitle       string
	MergedTitles       int
	AvgScore           float64
	WatchedEpisodesSum int
}

type groupedAnimeEntry struct {
	ID                 int
	Type               string
	GroupKey           string
	DisplayTitle       string
	MergedTitles       int
	AvgScore           float64
	WatchedEpisodesSum int
}

func (a *App) listAnime(userID int64) ([]AnimeItem, error) {
	anime := make([]AnimeItem, 0)

	err := a.withUserTx(context.Background(), userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		rows, err := tx.Query(`
			SELECT anime_id, anime_type, display_title, merged_titles, avg_score, watched_episodes_sum, synced_at
			FROM anime_entries
			ORDER BY CASE anime_type WHEN 'series' THEN 0 ELSE 1 END, anime_id
		`)
		if err != nil {
			return fmt.Errorf("query anime entries: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var (
				item     AnimeItem
				syncedAt time.Time
			)
			if err := rows.Scan(
				&item.ID,
				&item.Type,
				&item.DisplayTitle,
				&item.MergedTitles,
				&item.AvgScore,
				&item.WatchedEpisodesSum,
				&syncedAt,
			); err != nil {
				return fmt.Errorf("scan anime entries row: %w", err)
			}
			item.SyncedAt = syncedAt.UTC().Format(time.RFC3339)
			anime = append(anime, item)
		}

		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate anime entries rows: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return anime, nil
}

func (a *App) getStats(userID int64) (StatsResponse, error) {
	var stats StatsResponse

	err := a.withUserTx(context.Background(), userID, &sql.TxOptions{ReadOnly: true}, func(tx *sql.Tx) error {
		return tx.QueryRow(`
			SELECT
				COUNT(*) FILTER (WHERE anime_type = 'series'),
				COUNT(*) FILTER (WHERE anime_type = 'movie')
			FROM anime_entries
		`).Scan(&stats.SeriesCount, &stats.MoviesCount)
	})
	if err != nil {
		return StatsResponse{}, err
	}

	stats.TotalCount = stats.SeriesCount + stats.MoviesCount
	return stats, nil
}

func openDB(cfg AppConfig) (*sql.DB, error) {
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}

	db.SetMaxOpenConns(defaultMaxOpenDB)
	db.SetMaxIdleConns(defaultMaxIdleDB)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres database: %w", err)
	}

	return db, nil
}

func (a *App) withUserTx(ctx context.Context, userID int64, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
	ctx = ensureContext(ctx)

	if userID <= 0 {
		return fmt.Errorf("user_id must be positive")
	}

	tx, err := a.DB.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := setUserScope(ctx, tx, userID); err != nil {
		return fmt.Errorf("set user scope: %w", err)
	}
	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit()
}

func setUserScope(ctx context.Context, tx *sql.Tx, userID int64) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('`+userScopeSetting+`', $1, true)`, strconv.FormatInt(userID, 10))
	return err
}

func (a *App) saveGroupedLists(userID int64, seriesGroups, movieGroups []groupedView) error {
	return a.saveGroupedListsWithContext(context.Background(), userID, seriesGroups, movieGroups)
}

func (a *App) saveGroupedListsWithContext(ctx context.Context, userID int64, seriesGroups, movieGroups []groupedView) error {
	return a.saveGroupedEntriesWithContext(ctx, userID, flattenGroupedEntries(seriesGroups, movieGroups))
}

func flattenGroupedEntries(seriesGroups, movieGroups []groupedView) []groupedAnimeEntry {
	entries := make([]groupedAnimeEntry, 0, len(seriesGroups)+len(movieGroups))
	for _, g := range seriesGroups {
		entries = append(entries, groupedAnimeEntry{
			ID:                 g.ID,
			Type:               "series",
			GroupKey:           g.GroupKey,
			DisplayTitle:       g.DisplayTitle,
			MergedTitles:       g.MergedTitles,
			AvgScore:           g.AvgScore,
			WatchedEpisodesSum: g.WatchedEpisodesSum,
		})
	}
	for _, g := range movieGroups {
		entries = append(entries, groupedAnimeEntry{
			ID:                 g.ID,
			Type:               "movie",
			GroupKey:           g.GroupKey,
			DisplayTitle:       g.DisplayTitle,
			MergedTitles:       g.MergedTitles,
			AvgScore:           g.AvgScore,
			WatchedEpisodesSum: g.WatchedEpisodesSum,
		})
	}
	return entries
}

func (a *App) saveGroupedEntries(userID int64, entries []groupedAnimeEntry) error {
	return a.saveGroupedEntriesWithContext(context.Background(), userID, entries)
}

func (a *App) saveGroupedEntriesWithContext(ctx context.Context, userID int64, entries []groupedAnimeEntry) error {
	return a.withUserTx(ctx, userID, nil, func(tx *sql.Tx) error {
		return a.replaceEntriesWithContext(ctx, tx, userID, entries)
	})
}

func (a *App) replaceEntries(tx *sql.Tx, userID int64, entries []groupedAnimeEntry) error {
	return a.replaceEntriesWithContext(context.Background(), tx, userID, entries)
}

func (a *App) replaceEntriesWithContext(ctx context.Context, tx *sql.Tx, userID int64, entries []groupedAnimeEntry) error {
	ctx = ensureContext(ctx)

	a.logInfo("db", "rewriting user snapshot in DB table", "table", animeEntriesTableName, "user_id", userID, "rows", len(entries))

	if _, err := tx.ExecContext(ctx, `DELETE FROM anime_entries`); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO anime_entries (
			anime_id,
			anime_type,
			group_key,
			display_title,
			merged_titles,
			avg_score,
			watched_episodes_sum,
			synced_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	syncedAt := time.Now().UTC()
	for _, entry := range entries {
		if _, err := stmt.ExecContext(
			ctx,
			entry.ID,
			entry.Type,
			entry.GroupKey,
			entry.DisplayTitle,
			entry.MergedTitles,
			entry.AvgScore,
			entry.WatchedEpisodesSum,
			syncedAt,
		); err != nil {
			return err
		}
	}
	return nil
}
