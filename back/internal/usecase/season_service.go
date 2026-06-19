package usecase

import (
	"context"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

// SeasonQueryService serves the seasonal browse view. It reads only from the
// local catalog (the database is the single source of truth) and never calls
// MAL, so a season is as complete as the catalog hydrated by past syncs.
type SeasonQueryService struct {
	repo ports.SeasonReadRepository
	now  func() time.Time
}

func NewSeasonQueryService(repo ports.SeasonReadRepository) *SeasonQueryService {
	return &SeasonQueryService{repo: repo, now: time.Now}
}

// CurrentSeason reports the MAL season for the current moment.
func (service *SeasonQueryService) CurrentSeason() domain.Season {
	return domain.CurrentSeason(service.now())
}

// ListSeasonAnime validates the requested season and returns the catalog
// entries that premiered in it.
func (service *SeasonQueryService) ListSeasonAnime(ctx context.Context, year int, name string) (domain.Season, []domain.SeasonalAnimeItem, error) {
	season, err := domain.NewSeason(year, name)
	if err != nil {
		return domain.Season{}, nil, err
	}

	items, err := service.repo.ListSeasonAnime(ensureContext(ctx), season)
	if err != nil {
		return domain.Season{}, nil, err
	}

	return season, items, nil
}
