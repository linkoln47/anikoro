package usecase

import (
	"context"

	"test/internal/domain"
	"test/internal/ports"
)

type AnimeQueryService struct {
	repo ports.AnimeReadRepository
}

func NewAnimeQueryService(repo ports.AnimeReadRepository) *AnimeQueryService {
	return &AnimeQueryService{repo: repo}
}

func (service *AnimeQueryService) ListAnime(ctx context.Context, userID int64) ([]domain.AnimeListItem, error) {
	return service.repo.ListAnime(ensureContext(ctx), userID)
}

func (service *AnimeQueryService) GetStats(ctx context.Context, userID int64) (domain.AnimeStats, error) {
	return service.repo.GetStats(ensureContext(ctx), userID)
}

// GetFranchise returns the franchise grouping for a single anime id. A positive
// userID decorates the caller's list marks onto the result; userID 0 returns the
// same grouping with user-only fields zeroed, so the view works without a
// session.
func (service *AnimeQueryService) GetFranchise(ctx context.Context, animeID int, userID int64) (domain.AnimeListItem, bool, error) {
	return service.repo.GetFranchise(ensureContext(ctx), animeID, userID)
}

// ListFranchises returns every franchise group in the catalog for the
// catalog-wide browse grid. It is not scoped to a user.
func (service *AnimeQueryService) ListFranchises(ctx context.Context) ([]domain.FranchiseSummary, error) {
	return service.repo.ListFranchises(ensureContext(ctx))
}
