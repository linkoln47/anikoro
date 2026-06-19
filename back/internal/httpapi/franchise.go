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
