package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

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

func UserIDFromRequest(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(mux.Vars(r)["user_id"])
	if raw == "" {
		raw = strings.TrimSpace(r.Header.Get("X-User-ID"))
	}
	if raw == "" {
		raw = strings.TrimSpace(r.URL.Query().Get("user_id"))
	}
	if raw == "" {
		return 0, errors.New("user_id is required")
	}

	userID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || userID <= 0 {
		return 0, errors.New("user_id must be a positive integer")
	}

	return userID, nil
}

// API handlers
func (a *App) getAnimeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := UserIDFromRequest(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}

		anime, err := a.ListAnime(userID)
		if err != nil {
			a.logError("api", "failed to load anime list", "user_id", userID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load anime list: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, anime)
	}
}

func (a *App) syncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := UserIDFromRequest(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}

		token, err := a.getValidToken(userID)
		if err != nil {
			if errors.Is(err, ErrNoValidToken) || errors.Is(err, ErrTokenExpired) {
				a.logWarn("api", "sync rejected because token is unavailable", "user_id", userID, "err", err)
				writeAPIError(w, http.StatusUnauthorized, err.Error())
				return
			}

			a.logError("api", "failed to get valid token for sync", "user_id", userID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get valid token: %v", err))
			return
		}

		a.logInfo("api", "MAL sync requested", "user_id", userID)
		startSync := a.StartSync
		if startSync == nil {
			startSync = a.runSyncWithContext
		}
		go startSync(context.WithoutCancel(r.Context()), userID, token.AccessToken)

		response := SyncResponse{
			Success: true,
			Message: "Sync started in background",
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (a *App) getStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := UserIDFromRequest(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}

		response, err := a.GetStats(userID)
		if err != nil {
			a.logError("api", "failed to load stats", "user_id", userID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load stats: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (a *App) SetupRouter() *mux.Router {
	r := mux.NewRouter()

	// API routes
	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/anime/{user_id:[0-9]+}", a.getAnimeHandler()).Methods("GET")
	api.HandleFunc("/sync/{user_id:[0-9]+}", a.syncHandler()).Methods("POST")
	api.HandleFunc("/stats/{user_id:[0-9]+}", a.getStatsHandler()).Methods("GET")

	// Backward-compatible routes while clients migrate to path-based user ids.
	api.HandleFunc("/anime", a.getAnimeHandler()).Methods("GET")
	api.HandleFunc("/sync", a.syncHandler()).Methods("POST")
	api.HandleFunc("/stats", a.getStatsHandler()).Methods("GET")

	return r
}
