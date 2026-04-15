package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// API response structures
type AnimeItem struct {
	ID                 int     `json:"id"`
	DisplayTitle       string  `json:"display_title"`
	MergedTitles       int     `json:"merged_titles"`
	AvgScore           float64 `json:"avg_score"`
	WatchedEpisodesSum int     `json:"watched_episodes_sum"`
	SyncedAt           string  `json:"synced_at"`
	Type               string  `json:"type"` // "series" or "movie"
}

type SyncResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type StatsResponse struct {
	SeriesCount int `json:"series_count"`
	MoviesCount int `json:"movies_count"`
	TotalCount  int `json:"total_count"`
}

// API handlers
func (a *App) getAnimeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		anime, err := a.listAnime()
		if err != nil {
			a.logError("api", "failed to load anime list", "err", err)
			http.Error(w, fmt.Sprintf("Failed to load anime list: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(anime)
	}
}

func (a *App) syncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := a.getValidToken()
		if err != nil {
			if errors.Is(err, errNoValidToken) || errors.Is(err, errTokenRefreshFailed) {
				a.logWarn("api", "sync rejected because token is unavailable", "err", err)
				http.Error(w, err.Error(), http.StatusUnauthorized)
				return
			}

			a.logError("api", "failed to get valid token for sync", "err", err)
			http.Error(w, fmt.Sprintf("Failed to get valid token: %v", err), http.StatusInternalServerError)
			return
		}

		a.logInfo("api", "MAL sync requested")
		go a.runSync(token.AccessToken)

		response := SyncResponse{
			Success: true,
			Message: "Sync started in background",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func (a *App) getStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := a.getStats()
		if err != nil {
			a.logError("api", "failed to load stats", "err", err)
			http.Error(w, fmt.Sprintf("Failed to load stats: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

func (a *App) setupRouter() *mux.Router {
	r := mux.NewRouter()

	// API routes
	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/anime", a.getAnimeHandler()).Methods("GET")
	api.HandleFunc("/sync", a.syncHandler()).Methods("POST")
	api.HandleFunc("/stats", a.getStatsHandler()).Methods("GET")

	return r
}
