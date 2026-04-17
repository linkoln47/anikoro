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

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

func writeAPIError(w http.ResponseWriter, status int, message string) {
	http.Error(w, message, status)
}

// API handlers
func (a *App) getAnimeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		anime, err := a.listAnime()
		if err != nil {
			a.logError("api", "failed to load anime list", "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load anime list: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, anime)
	}
}

func (a *App) syncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := a.getValidToken()
		if err != nil {
			if errors.Is(err, errNoValidToken) || errors.Is(err, errTokenRefreshFailed) {
				a.logWarn("api", "sync rejected because token is unavailable", "err", err)
				writeAPIError(w, http.StatusUnauthorized, err.Error())
				return
			}

			a.logError("api", "failed to get valid token for sync", "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get valid token: %v", err))
			return
		}

		a.logInfo("api", "MAL sync requested")
		startSync := a.StartSync
		if startSync == nil {
			startSync = a.runSync
		}
		go startSync(token.AccessToken)

		response := SyncResponse{
			Success: true,
			Message: "Sync started in background",
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (a *App) getStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		response, err := a.getStats()
		if err != nil {
			a.logError("api", "failed to load stats", "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load stats: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, response)
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
