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
	ID             int      `json:"id"`
	Title          string   `json:"title"`
	MediaType      string   `json:"media_type"`
	StartDate      string   `json:"start_date,omitempty"`
	ImageMediumURL string   `json:"image_medium_url,omitempty"`
	ImageLargeURL  string   `json:"image_large_url,omitempty"`
	NumEpisodes    int      `json:"num_episodes,omitempty"`
	MemberCount    int      `json:"member_count"`
	Score          *float64 `json:"score,omitempty"`
}

// FranchiseListResponse is a single page of the catalog-wide franchise grid:
// the windowed items plus the total number of groups matching the filters, so
// the client can drive its paging/virtualized scroll.
type FranchiseListResponse struct {
	Items []FranchiseSummaryItem `json:"items"`
	Total int                    `json:"total"`
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
			Score:          item.Score,
		})
	}
	return response
}

// franchiseMediaTypes is the set of representative media types the "all anime"
// grid can filter by. It mirrors the values MAL stores in anime_catalog.media_type
// and the labels the front-end renders; an unknown value is rejected rather than
// silently returning everything.
var franchiseMediaTypes = map[string]struct{}{
	"tv":      {},
	"movie":   {},
	"ova":     {},
	"ona":     {},
	"special": {},
	"music":   {},
}

const (
	franchisePageDefaultLimit = 48
	franchisePageMaxLimit     = 100
	franchiseSearchMaxLength  = 100
)

// listFranchisesHandler returns one filtered, paginated page of the catalog-wide
// franchise grid for the "all anime" browse view. Filtering by representative
// media type and title and paging both happen server-side so the page never has
// to load the whole catalog at once. Like the single franchise view it needs no
// session: it reads only the global catalog, so the page works with or without a
// signed-in user.
func (api *HTTPAPI) listFranchisesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		mediaType := strings.ToLower(strings.TrimSpace(query.Get("media_type")))
		if mediaType != "" {
			if _, ok := franchiseMediaTypes[mediaType]; !ok {
				writeAPIError(w, http.StatusBadRequest, "media_type must be one of tv, movie, ova, ona, special, music")
				return
			}
		}

		search := strings.TrimSpace(query.Get("q"))
		if len(search) > franchiseSearchMaxLength {
			search = search[:franchiseSearchMaxLength]
		}

		limit := franchisePageDefaultLimit
		if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeAPIError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			if parsed > franchisePageMaxLimit {
				parsed = franchisePageMaxLimit
			}
			limit = parsed
		}

		offset := 0
		if raw := strings.TrimSpace(query.Get("offset")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				writeAPIError(w, http.StatusBadRequest, "offset must be a non-negative integer")
				return
			}
			offset = parsed
		}

		franchises, total, err := api.animeQueries.ListFranchises(r.Context(), domain.FranchiseQuery{
			MediaType: mediaType,
			Search:    search,
			Limit:     limit,
			Offset:    offset,
		})
		if err != nil {
			api.logError("api", "failed to list franchises", "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load franchises: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, FranchiseListResponse{
			Items: toFranchiseSummaryResponse(franchises),
			Total: total,
		})
	}
}
