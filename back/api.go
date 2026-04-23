package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

// API response structures
type FranchiseItem struct {
	ID                    int    `json:"id"`
	Title                 string `json:"title"`
	MediaType             string `json:"media_type"`
	StartDate             string `json:"start_date,omitempty"`
	ImageMediumURL        string `json:"image_medium_url,omitempty"`
	ImageLargeURL         string `json:"image_large_url,omitempty"`
	RelationType          string `json:"relation_type,omitempty"`
	RelationTypeFormatted string `json:"relation_type_formatted,omitempty"`
	InUserList            bool   `json:"in_user_list"`
	UserScore             int    `json:"user_score,omitempty"`
	WatchedEpisodes       int    `json:"watched_episodes,omitempty"`
}

type AnimeItem struct {
	ID                 int             `json:"id"`
	DisplayTitle       string          `json:"display_title"`
	MergedTitles       int             `json:"merged_titles"`
	AvgScore           float64         `json:"avg_score"`
	WatchedEpisodesSum int             `json:"watched_episodes_sum"`
	SyncedAt           string          `json:"synced_at"`
	Type               string          `json:"type"` // "series" or "movie"
	Franchise          []FranchiseItem `json:"franchise"`
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

func writeAuthError(w http.ResponseWriter) {
	writeAPIError(w, http.StatusUnauthorized, ErrUnauthenticated.Error())
}

// API handlers
func (a *App) getAnimeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		anime, err := a.ListAnime(user.ID)
		if err != nil {
			a.logError("api", "failed to load anime list", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load anime list: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, anime)
	}
}

func (a *App) syncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		token, err := a.getValidToken(user.ID)
		if err != nil {
			if errors.Is(err, ErrNoValidToken) || errors.Is(err, ErrTokenExpired) {
				a.logWarn("api", "sync rejected because token is unavailable", "username", user.Username, "user_id", user.ID, "err", err)
				writeAPIError(w, http.StatusUnauthorized, err.Error())
				return
			}

			a.logError("api", "failed to get valid token for sync", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get valid token: %v", err))
			return
		}

		a.logInfo("api", "MAL sync requested", "username", user.Username, "user_id", user.ID)
		go a.runSyncWithContext(context.WithoutCancel(r.Context()), user.ID, token.AccessToken)

		response := SyncResponse{
			Success: true,
			Message: "Sync started in background",
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (a *App) getStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		response, err := a.GetStats(user.ID)
		if err != nil {
			a.logError("api", "failed to load stats", "username", user.Username, "user_id", user.ID, "err", err)
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
	api.HandleFunc("/auth/mal/start", a.startMALAuthHandler()).Methods("GET")
	api.HandleFunc("/auth/mal/callback", a.completeMALAuthHandler()).Methods("GET")
	api.HandleFunc("/auth/logout", a.logoutHandler()).Methods("POST")
	api.HandleFunc("/me", a.meHandler()).Methods("GET")
	api.HandleFunc("/anime", a.getAnimeHandler()).Methods("GET")
	api.HandleFunc("/sync", a.syncHandler()).Methods("POST")
	api.HandleFunc("/stats", a.getStatsHandler()).Methods("GET")

	return r
}
