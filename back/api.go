package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

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
	JobID   string `json:"job_id,omitempty"`
}

type StatsResponse struct {
	SeriesCount int `json:"series_count"`
	MoviesCount int `json:"movies_count"`
	TotalCount  int `json:"total_count"`
}

type PublicSyncRequest struct {
	Username string `json:"username"`
}

var ErrPublicUsernameRequired = errors.New("username is required")

func toAnimeResponse(items []AnimeListItem) []AnimeItem {
	response := make([]AnimeItem, 0, len(items))
	for _, item := range items {
		response = append(response, AnimeItem{
			ID:                 item.ID,
			DisplayTitle:       item.DisplayTitle,
			MergedTitles:       item.MergedTitles,
			AvgScore:           item.AvgScore,
			WatchedEpisodesSum: item.WatchedEpisodesSum,
			SyncedAt:           item.SyncedAt,
			Type:               item.Type,
			Franchise:          toFranchiseResponse(item.Franchise),
		})
	}
	return response
}

func toFranchiseResponse(entries []FranchiseEntry) []FranchiseItem {
	response := make([]FranchiseItem, 0, len(entries))
	for _, entry := range entries {
		response = append(response, FranchiseItem{
			ID:                    entry.ID,
			Title:                 entry.Title,
			MediaType:             entry.MediaType,
			StartDate:             entry.StartDate,
			ImageMediumURL:        entry.ImageMediumURL,
			ImageLargeURL:         entry.ImageLargeURL,
			RelationType:          entry.RelationType,
			RelationTypeFormatted: entry.RelationTypeFormatted,
			InUserList:            entry.InUserList,
			UserScore:             entry.UserScore,
			WatchedEpisodes:       entry.WatchedEpisodes,
		})
	}
	return response
}

func toStatsResponse(stats AnimeStats) StatsResponse {
	return StatsResponse{
		SeriesCount: stats.SeriesCount,
		MoviesCount: stats.MoviesCount,
		TotalCount:  stats.TotalCount,
	}
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
func (api *HTTPAPI) getAnimeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		anime, err := api.animeQueries.ListAnime(r.Context(), user.ID)
		if err != nil {
			api.logError("api", "failed to load anime list", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load anime list: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, toAnimeResponse(anime))
	}
}

func (api *HTTPAPI) getPublicAnimeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.publicUserFromPath(r)
		if err != nil {
			api.writePublicUserLookupError(w, err)
			return
		}

		anime, err := api.animeQueries.ListAnime(r.Context(), user.ID)
		if err != nil {
			api.logError("api", "failed to load public anime list", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load anime list: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, toAnimeResponse(anime))
	}
}

func (api *HTTPAPI) syncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		token, err := api.auth.getValidToken(user.ID)
		if err != nil {
			if errors.Is(err, ErrNoValidToken) || errors.Is(err, ErrTokenExpired) {
				api.logWarn("api", "sync rejected because token is unavailable", "username", user.Username, "user_id", user.ID, "err", err)
				writeAPIError(w, http.StatusUnauthorized, err.Error())
				return
			}

			api.logError("api", "failed to get valid token for sync", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get valid token: %v", err))
			return
		}

		job, err := api.syncJobs.Create(user.ID, user.Username, syncJobModeSession)
		if err != nil {
			api.logError("api", "failed to create sync job", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create sync job: %v", err))
			return
		}

		api.logInfo("api", "MAL sync requested", "username", user.Username, "user_id", user.ID)
		go api.sync.RunSyncWithJob(context.WithoutCancel(r.Context()), user.ID, token.AccessToken, job)

		response := SyncResponse{
			Success: true,
			Message: "Sync started in background",
			JobID:   job.Snapshot().ID,
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (api *HTTPAPI) publicSyncHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, err := publicSyncUsernameFromRequest(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(api.config.ClientID) == "" {
			writeAPIError(w, http.StatusInternalServerError, "MAL_CLIENT_ID is required for public sync")
			return
		}

		user, err := api.auth.upsertPublicUser(username)
		if err != nil {
			api.logError("api", "failed to upsert public MAL user", "username", username, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save user: %v", err))
			return
		}

		job, err := api.syncJobs.Create(user.ID, user.Username, syncJobModePublic)
		if err != nil {
			api.logError("api", "failed to create public sync job", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create sync job: %v", err))
			return
		}

		api.logInfo("api", "public MAL sync requested", "username", user.Username, "user_id", user.ID)
		go api.sync.RunPublicSyncWithJob(context.WithoutCancel(r.Context()), user.ID, user.Username, job)

		writeJSON(w, http.StatusOK, SyncResponse{
			Success: true,
			Message: "Public sync started in background",
			JobID:   job.Snapshot().ID,
		})
	}
}

func (api *HTTPAPI) getStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		response, err := api.animeQueries.GetStats(r.Context(), user.ID)
		if err != nil {
			api.logError("api", "failed to load stats", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load stats: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, toStatsResponse(response))
	}
}

func (api *HTTPAPI) getPublicStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.publicUserFromPath(r)
		if err != nil {
			api.writePublicUserLookupError(w, err)
			return
		}

		response, err := api.animeQueries.GetStats(r.Context(), user.ID)
		if err != nil {
			api.logError("api", "failed to load public stats", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load stats: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, toStatsResponse(response))
	}
}

func publicSyncUsernameFromRequest(r *http.Request) (string, error) {
	var request PublicSyncRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		return "", fmt.Errorf("invalid JSON body: %w", err)
	}

	username := strings.TrimSpace(request.Username)
	if username == "" {
		return "", ErrPublicUsernameRequired
	}

	return username, nil
}

func (api *HTTPAPI) publicUserFromPath(r *http.Request) (User, error) {
	username := strings.TrimSpace(mux.Vars(r)["username"])
	if username == "" {
		return User{}, ErrPublicUsernameRequired
	}

	return api.auth.userByUsername(username)
}

func (api *HTTPAPI) writePublicUserLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPublicUsernameRequired):
		writeAPIError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrUserNotFound):
		writeAPIError(w, http.StatusNotFound, "public user snapshot not found; run public sync first")
	default:
		writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to resolve public user: %v", err))
	}
}

func (api *HTTPAPI) getSyncJobHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job, err := api.syncJobFromRequest(r)
		if err != nil {
			api.writeSyncJobLookupError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, job.Snapshot())
	}
}

func (api *HTTPAPI) syncJobEventsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job, err := api.syncJobFromRequest(r)
		if err != nil {
			api.writeSyncJobLookupError(w, err)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeAPIError(w, http.StatusInternalServerError, "streaming is not supported")
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		updates, unsubscribe := job.Subscribe()
		defer unsubscribe()

		for {
			select {
			case <-r.Context().Done():
				return
			case snapshot, ok := <-updates:
				if !ok {
					return
				}
				if err := writeSSESnapshot(w, snapshot); err != nil {
					return
				}
				flusher.Flush()
				if syncJobIsFinal(snapshot.Status) {
					return
				}
			}
		}
	}
}

func (api *HTTPAPI) writeSyncJobLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSyncJobNotFound):
		writeAPIError(w, http.StatusNotFound, err.Error())
	default:
		writeAPIError(w, http.StatusBadRequest, err.Error())
	}
}

func (api *HTTPAPI) SetupRouter() *mux.Router {
	r := mux.NewRouter()

	// API routes
	routes := r.PathPrefix("/api").Subrouter()
	routes.HandleFunc("/auth/mal/start", api.startMALAuthHandler()).Methods("GET")
	routes.HandleFunc("/auth/mal/callback", api.completeMALAuthHandler()).Methods("GET")
	routes.HandleFunc("/auth/logout", api.logoutHandler()).Methods("POST")
	routes.HandleFunc("/me", api.meHandler()).Methods("GET")
	routes.HandleFunc("/anime", api.getAnimeHandler()).Methods("GET")
	routes.HandleFunc("/sync", api.syncHandler()).Methods("POST")
	routes.HandleFunc("/sync/jobs/{job_id}", api.getSyncJobHandler()).Methods("GET")
	routes.HandleFunc("/sync/jobs/{job_id}/events", api.syncJobEventsHandler()).Methods("GET")
	routes.HandleFunc("/stats", api.getStatsHandler()).Methods("GET")
	routes.HandleFunc("/public/sync", api.publicSyncHandler()).Methods("POST")
	routes.HandleFunc("/public/anime/{username}", api.getPublicAnimeHandler()).Methods("GET")
	routes.HandleFunc("/public/stats/{username}", api.getPublicStatsHandler()).Methods("GET")

	return r
}
