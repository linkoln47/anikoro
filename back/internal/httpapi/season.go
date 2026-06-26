package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"test/internal/domain"
)

type SeasonalAnimeItem struct {
	ID             int         `json:"id"`
	Title          string      `json:"title"`
	MediaType      string      `json:"media_type"`
	StartDate      string      `json:"start_date,omitempty"`
	ImageMediumURL string      `json:"image_medium_url,omitempty"`
	ImageLargeURL  string      `json:"image_large_url,omitempty"`
	NumEpisodes    int         `json:"num_episodes,omitempty"`
	MeanScore      *float64    `json:"mean_score,omitempty"`
	Genres         []GenreItem `json:"genres,omitempty"`
}

type SeasonResponse struct {
	Year   int                 `json:"year"`
	Season string              `json:"season"`
	Anime  []SeasonalAnimeItem `json:"anime"`
}

func toSeasonResponse(season domain.Season, items []domain.SeasonalAnimeItem) SeasonResponse {
	anime := make([]SeasonalAnimeItem, 0, len(items))
	for _, item := range items {
		anime = append(anime, SeasonalAnimeItem{
			ID:             item.ID,
			Title:          item.Title,
			MediaType:      item.MediaType,
			StartDate:      item.StartDate,
			ImageMediumURL: item.ImageMediumURL,
			ImageLargeURL:  item.ImageLargeURL,
			NumEpisodes:    item.NumEpisodes,
			MeanScore:      item.MeanScore,
			Genres:         toGenreResponse(item.Genres),
		})
	}

	return SeasonResponse{
		Year:   season.Year,
		Season: string(season.Name),
		Anime:  anime,
	}
}

func (api *HTTPAPI) getCurrentSeasonHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		current := api.seasonQueries.CurrentSeason()
		api.writeSeason(w, r, current.Year, string(current.Name))
	}
}

func (api *HTTPAPI) getSeasonHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		year, err := strconv.Atoi(strings.TrimSpace(vars["year"]))
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "year must be an integer")
			return
		}

		api.writeSeason(w, r, year, vars["season"])
	}
}

func (api *HTTPAPI) writeSeason(w http.ResponseWriter, r *http.Request, year int, name string) {
	season, items, err := api.seasonQueries.ListSeasonAnime(r.Context(), year, name)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidSeason) {
			writeAPIError(w, http.StatusBadRequest, err.Error())
			return
		}

		api.logError("api", "failed to load season anime", "year", year, "season", name, "err", err)
		writeAPIError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to load season anime: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, toSeasonResponse(season, items))
}
