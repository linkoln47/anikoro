package usecase

import "context"

import "test/internal/ports"

type AnimeQueryService struct {
	repo ports.AnimeReadRepository
}

func NewAnimeQueryService(repo ports.AnimeReadRepository) *AnimeQueryService {
	return &AnimeQueryService{repo: repo}
}

func (service *AnimeQueryService) ListAnime(ctx context.Context, userID int64) ([]AnimeListItem, error) {
	return service.repo.ListAnime(ensureContext(ctx), userID)
}

func (service *AnimeQueryService) GetStats(ctx context.Context, userID int64) (AnimeStats, error) {
	return service.repo.GetStats(ensureContext(ctx), userID)
}
