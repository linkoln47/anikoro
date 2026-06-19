package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"test/internal/domain"
)

// getFranchiseHandler returns the global franchise grouping for a single anime
// id without any user-list data. It is public and powers the seasonal browse
// view, where anime are not tied to the signed-in user's list.
func (api *HTTPAPI) getFranchiseHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		animeID, err := strconv.Atoi(strings.TrimSpace(mux.Vars(r)["anime_id"]))
		if err != nil || animeID <= 0 {
			writeAPIError(w, http.StatusBadRequest, "anime id must be a positive integer")
			return
		}

		franchise, ok, err := api.franchiseQueries.GetFranchise(r.Context(), animeID)
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
