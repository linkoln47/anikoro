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
