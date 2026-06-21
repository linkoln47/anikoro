package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"test/internal/domain"
)

// getFranchiseHandler returns the franchise grouping for a single anime id. It
// powers the seasonal browse view and works without a session: anonymous
// callers get the global grouping, while a signed-in caller additionally sees
// their own list marks decorated onto it. Authorization only widens what the
// same entity exposes; it is never required.
func (api *HTTPAPI) getFranchiseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		animeID, err := strconv.Atoi(strings.TrimSpace(mux.Vars(r)["anime_id"]))
		if err != nil || animeID <= 0 {
			writeAPIError(w, http.StatusBadRequest, "anime id must be a positive integer")
			return
		}

		var userID int64
		if user, userErr := api.currentUserFromRequest(r); userErr == nil {
			userID = user.ID
		}

		franchise, ok, err := api.animeQueries.GetFranchise(r.Context(), animeID, userID)
		if err != nil {
			api.logError("api", "failed to load franchise", "anime_id", animeID, "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load franchise: %v", err))
			return
		}
		if !ok {
			writeAPIError(w, http.StatusNotFound, "anime not found in catalog")
			return
		}

		items := toAnimeResponse([]domain.AnimeListItem{franchise})
		writeJSON(w, http.StatusOK, items[0])
	}
}

type FranchiseSummaryItem struct {
	ID             int    `json:"id"`
	Title          string `json:"title"`
	MediaType      string `json:"media_type"`
	StartDate      string `json:"start_date,omitempty"`
	ImageMediumURL string `json:"image_medium_url,omitempty"`
	ImageLargeURL  string `json:"image_large_url,omitempty"`
	NumEpisodes    int    `json:"num_episodes,omitempty"`
	MemberCount    int    `json:"member_count"`
}

func toFranchiseSummaryResponse(items []domain.FranchiseSummary) []FranchiseSummaryItem {
	response := make([]FranchiseSummaryItem, 0, len(items))
	for _, item := range items {
		response = append(response, FranchiseSummaryItem{
			ID:             item.ID,
			Title:          item.Title,
			MediaType:      item.MediaType,
			StartDate:      item.StartDate,
			ImageMediumURL: item.ImageMediumURL,
			ImageLargeURL:  item.ImageLargeURL,
			NumEpisodes:    item.NumEpisodes,
			MemberCount:    item.MemberCount,
		})
	}
	return response
}

// listFranchisesHandler returns every franchise group in the catalog reduced to
// its representative title for the "all anime" browse grid. Like the single
// franchise view it needs no session: it reads only the global catalog, so the
// page works with or without a signed-in user.
func (api *HTTPAPI) listFranchisesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		franchises, err := api.animeQueries.ListFranchises(r.Context())
		if err != nil {
			api.logError("api", "failed to list franchises", "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load franchises: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, toFranchiseSummaryResponse(franchises))
	}
}
