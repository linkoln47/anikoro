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

// GenreListResponse carries the catalog's genre universe for the franchise grid's
// genre filter.
type GenreListResponse struct {
	Genres []GenreItem `json:"genres"`
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

		sort := strings.ToLower(strings.TrimSpace(query.Get("sort")))
		if _, ok := domain.FranchiseSortColumn(sort); !ok {
			writeAPIError(w, http.StatusBadRequest, "sort must be one of score, title, date, episodes")
			return
		}

		genreIDs, err := parseGenreIDs(query.Get("genres"))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "genres must be a comma-separated list of positive integers")
			return
		}

		// R18+ content is gated off by default; the client opts in with adult=1/true.
		includeAdult := isTruthyParam(query.Get("adult"))

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
			MediaType:    mediaType,
			Search:       search,
			Sort:         sort,
			GenreIDs:     genreIDs,
			IncludeAdult: includeAdult,
			Limit:        limit,
			Offset:       offset,
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

// franchiseMaxGenreFilters caps how many genre ids a single request may filter by,
// so a crafted query cannot fan the IN-clause out unbounded.
const franchiseMaxGenreFilters = 50

// parseGenreIDs parses the comma-separated "genres" query parameter into a list of
// positive genre ids. An empty value yields no filter; any non-positive or
// non-numeric entry is an error so a malformed filter is rejected rather than
// silently ignored.
func parseGenreIDs(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid genre id %q", part)
		}
		ids = append(ids, id)
		if len(ids) > franchiseMaxGenreFilters {
			return nil, fmt.Errorf("too many genre filters")
		}
	}

	return ids, nil
}

// isTruthyParam reports whether a query parameter signals "on" (1/true/yes).
func isTruthyParam(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// listGenresHandler returns the catalog's genre universe for the "all franchises"
// genre filter. Like the franchise listing it reads only the global catalog and
// needs no session.
func (api *HTTPAPI) listGenresHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		genres, err := api.animeQueries.ListGenres(r.Context())
		if err != nil {
			api.logError("api", "failed to list genres", "err", err)
			writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load genres: %v", err))
			return
		}

		writeJSON(w, http.StatusOK, GenreListResponse{Genres: toGenreResponse(genres)})
	}
}
