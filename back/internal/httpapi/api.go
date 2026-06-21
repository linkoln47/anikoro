package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"test/internal/domain"
	"test/internal/usecase"
)

type FranchiseItem struct {
	ID                    int    `json:"id"`
	Title                 string `json:"title"`
	MediaType             string `json:"media_type"`
	StartDate             string `json:"start_date,omitempty"`
	ImageMediumURL        string `json:"image_medium_url,omitempty"`
	ImageLargeURL         string `json:"image_large_url,omitempty"`
	NumEpisodes           int    `json:"num_episodes,omitempty"`
	RelationType          string `json:"relation_type,omitempty"`
	RelationTypeFormatted string `json:"relation_type_formatted,omitempty"`
	InUserList            bool   `json:"in_user_list"`
	UserScore             int    `json:"user_score,omitempty"`
	WatchedEpisodes       int    `json:"watched_episodes,omitempty"`
	UserListStatus        string `json:"user_list_status,omitempty"`
}

type AnimeItem struct {
	ID                 int               `json:"id"`
	DisplayTitle       string            `json:"display_title"`
	MergedTitles       int               `json:"merged_titles"`
	AvgScore           float64           `json:"avg_score"`
	WatchedEpisodesSum int               `json:"watched_episodes_sum"`
	SyncedAt           string            `json:"synced_at"`
	Type               string            `json:"type"`
	StatusCounts       AnimeStatusCounts `json:"status_counts"`
	Franchise          []FranchiseItem   `json:"franchise"`
}

type SyncResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	JobID   string `json:"job_id,omitempty"`
}

type StatsResponse struct {
	SeriesCount  int               `json:"series_count"`
	MoviesCount  int               `json:"movies_count"`
	TotalCount   int               `json:"total_count"`
	StatusCounts AnimeStatusCounts `json:"status_counts"`
}

type AnimeStatusCounts struct {
	Watching    int `json:"watching"`
	Completed   int `json:"completed"`
	OnHold      int `json:"on_hold"`
	Dropped     int `json:"dropped"`
	PlanToWatch int `json:"plan_to_watch"`
}

var ErrPublicUsernameRequired = errors.New("username is required")

func toAnimeResponse(items []domain.AnimeListItem) []AnimeItem {
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
			StatusCounts:       toAnimeStatusCounts(item.StatusCounts),
			Franchise:          toFranchiseResponse(item.Franchise),
		})
	}
	return response
}

func toFranchiseResponse(entries []domain.FranchiseEntry) []FranchiseItem {
	response := make([]FranchiseItem, 0, len(entries))
	for _, entry := range entries {
		response = append(response, FranchiseItem{
			ID:                    entry.ID,
			Title:                 entry.Title,
			MediaType:             entry.MediaType,
			StartDate:             entry.StartDate,
			ImageMediumURL:        entry.ImageMediumURL,
			ImageLargeURL:         entry.ImageLargeURL,
			NumEpisodes:           entry.NumEpisodes,
			RelationType:          entry.RelationType,
			RelationTypeFormatted: entry.RelationTypeFormatted,
			InUserList:            entry.InUserList,
			UserScore:             entry.UserScore,
			WatchedEpisodes:       entry.WatchedEpisodes,
			UserListStatus:        entry.UserListStatus,
		})
	}
	return response
}

func toStatsResponse(stats domain.AnimeStats) StatsResponse {
	return StatsResponse{
		SeriesCount:  stats.SeriesCount,
		MoviesCount:  stats.MoviesCount,
		TotalCount:   stats.TotalCount,
		StatusCounts: toAnimeStatusCounts(stats.StatusCounts),
	}
}

func toAnimeStatusCounts(counts map[string]int) AnimeStatusCounts {
	return AnimeStatusCounts{
		Watching:    counts[string(domain.AnimeListStatusWatching)],
		Completed:   counts[string(domain.AnimeListStatusCompleted)],
		OnHold:      counts[string(domain.AnimeListStatusOnHold)],
		Dropped:     counts[string(domain.AnimeListStatusDropped)],
		PlanToWatch: counts[string(domain.AnimeListStatusPlanToWatch)],
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
		user, err := api.userFromUsernamePath(r)
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

		token, err := api.auth.GetValidToken(r.Context(), user.ID)
		if err != nil {
			if errors.Is(err, usecase.ErrNoValidToken) || errors.Is(err, usecase.ErrTokenExpired) {
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
			JobID:   job.snapshotCopy().ID,
		}

		writeJSON(w, http.StatusOK, response)
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
		user, err := api.userFromUsernamePath(r)
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

func (api *HTTPAPI) userFromUsernamePath(r *http.Request) (domain.User, error) {
	username := strings.TrimSpace(mux.Vars(r)["username"])
	if username == "" {
		return domain.User{}, ErrPublicUsernameRequired
	}

	return api.auth.ResolveUserByUsername(r.Context(), username)
}

func (api *HTTPAPI) writePublicUserLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrPublicUsernameRequired):
		writeAPIError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, usecase.ErrUserNotFound):
		writeAPIError(w, http.StatusNotFound, "user snapshot not found; run public sync first")
	default:
		writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to resolve public user: %v", err))
	}
}

func (api *HTTPAPI) getSyncJobHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job, scope, err := api.syncJobFromRequest(r)
		if err != nil {
			api.writeSyncJobLookupError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, newSyncJobResponse(job.snapshotCopy(), scope))
	}
}

func (api *HTTPAPI) syncJobEventsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		job, scope, err := api.syncJobFromRequest(r)
		if err != nil {
			api.writeSyncJobLookupError(w, err)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeAPIError(w, http.StatusInternalServerError, "streaming is not supported")
			return
		}

		if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
			api.logWarn("api", "cannot clear SSE write deadline", "err", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		updates, unsubscribe := job.subscribe()
		defer unsubscribe()

		for {
			select {
			case <-r.Context().Done():
				return
			case snapshot, ok := <-updates:
				if !ok {
					return
				}
				if err := writeSSESnapshot(w, snapshot, scope); err != nil {
					return
				}
				flusher.Flush()
				if syncJobProgressIsFinal(snapshot) {
					return
				}
			}
		}
	}
}

func (api *HTTPAPI) writeSyncJobLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		writeAuthError(w)
	case errors.Is(err, ErrSyncJobForbidden):
		writeAPIError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrSyncJobNotFound):
		writeAPIError(w, http.StatusNotFound, err.Error())
	default:
		writeAPIError(w, http.StatusBadRequest, err.Error())
	}
}
