package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"test/internal/domain"
	"test/internal/usecase"
)

// UpdateListEntryRequest carries a partial list entry update. Nil fields are
// left unchanged on MAL and in the local snapshot.
type UpdateListEntryRequest struct {
	Status             *string `json:"status,omitempty"`
	Score              *int    `json:"score,omitempty"`
	NumWatchedEpisodes *int    `json:"num_watched_episodes,omitempty"`
}

type ListEntryResponse struct {
	AnimeID            int    `json:"anime_id"`
	Title              string `json:"title"`
	Status             string `json:"status"`
	Score              int    `json:"score"`
	NumWatchedEpisodes int    `json:"num_watched_episodes"`
	NumEpisodes        int    `json:"num_episodes"`
}

type RemovedListEntryResponse struct {
	AnimeID int  `json:"anime_id"`
	Removed bool `json:"removed"`
}

func (api *HTTPAPI) updateListEntryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		animeID, err := animeIDFromRequestPath(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}

		patch, err := listEntryPatchFromRequest(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}

		token, err := api.auth.GetValidToken(r.Context(), user.ID)
		if err != nil {
			if errors.Is(err, usecase.ErrNoValidToken) || errors.Is(err, usecase.ErrTokenExpired) {
				api.logWarn("api", "list edit rejected because token is unavailable", "username", user.Username, "user_id", user.ID, "err", err)
				writeAPIError(w, http.StatusUnauthorized, err.Error())
				return
			}

			api.logError("api", "failed to get valid token for list edit", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get valid token: %v", err))
			return
		}

		updated, err := api.listEdits.UpdateUserAnimeListEntry(r.Context(), user.ID, token.AccessToken, animeID, patch)
		if err != nil {
			api.writeListEditError(w, user.Username, user.ID, animeID, err)
			return
		}

		api.logInfo("api", "anime list entry updated", "username", user.Username, "user_id", user.ID, "anime_id", animeID)
		writeJSON(w, http.StatusOK, ListEntryResponse{
			AnimeID:            updated.AnimeID,
			Title:              updated.Title,
			Status:             string(updated.ListStatus),
			Score:              updated.Score,
			NumWatchedEpisodes: updated.WatchedEpisodes,
			NumEpisodes:        updated.NumEpisodes,
		})
	}
}

func (api *HTTPAPI) removeListEntryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := api.currentUserFromRequest(r)
		if err != nil {
			writeAuthError(w)
			return
		}

		animeID, err := animeIDFromRequestPath(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}

		token, err := api.auth.GetValidToken(r.Context(), user.ID)
		if err != nil {
			if errors.Is(err, usecase.ErrNoValidToken) || errors.Is(err, usecase.ErrTokenExpired) {
				api.logWarn("api", "list entry removal rejected because token is unavailable", "username", user.Username, "user_id", user.ID, "err", err)
				writeAPIError(w, http.StatusUnauthorized, err.Error())
				return
			}

			api.logError("api", "failed to get valid token for list entry removal", "username", user.Username, "user_id", user.ID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get valid token: %v", err))
			return
		}

		if err := api.listEdits.RemoveUserAnimeListEntry(r.Context(), user.ID, token.AccessToken, animeID); err != nil {
			api.writeListEditError(w, user.Username, user.ID, animeID, err)
			return
		}

		api.logInfo("api", "anime list entry removed", "username", user.Username, "user_id", user.ID, "anime_id", animeID)
		writeJSON(w, http.StatusOK, RemovedListEntryResponse{
			AnimeID: animeID,
			Removed: true,
		})
	}
}

func (api *HTTPAPI) writeListEditError(w http.ResponseWriter, username string, userID int64, animeID int, err error) {
	switch {
	case errors.Is(err, usecase.ErrInvalidListEditInput):
		writeAPIError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, usecase.ErrAnimeNotInCatalog):
		writeAPIError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, usecase.ErrMALListUpdateFailed):
		api.logError("api", "failed to update MAL list entry", "username", username, "user_id", userID, "anime_id", animeID, "err", err)
		writeAPIError(w, http.StatusBadGateway, "Failed to update the entry on MAL")
	default:
		api.logError("api", "failed to update anime list entry", "username", username, "user_id", userID, "anime_id", animeID, "err", err)
		writeAPIError(w, http.StatusInternalServerError, "Failed to update anime list entry")
	}
}

func animeIDFromRequestPath(r *http.Request) (int, error) {
	raw := strings.TrimSpace(mux.Vars(r)["anime_id"])
	animeID, err := strconv.Atoi(raw)
	if err != nil || animeID <= 0 {
		return 0, errors.New("anime_id must be a positive integer")
	}
	return animeID, nil
}

func listEntryPatchFromRequest(r *http.Request) (domain.UserAnimeListPatch, error) {
	var request UpdateListEntryRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return domain.UserAnimeListPatch{}, fmt.Errorf("invalid JSON body: %w", err)
	}

	patch := domain.UserAnimeListPatch{
		Score:           request.Score,
		WatchedEpisodes: request.NumWatchedEpisodes,
	}
	if request.Status != nil {
		status, ok := domain.NormalizeAnimeListStatus(*request.Status)
		if !ok {
			return domain.UserAnimeListPatch{}, fmt.Errorf("%w: %q", domain.ErrInvalidAnimeListStatus, *request.Status)
		}
		patch.Status = &status
	}
	if patch.IsEmpty() {
		return domain.UserAnimeListPatch{}, domain.ErrEmptyAnimeListPatch
	}

	return patch, nil
}
