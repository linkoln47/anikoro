package usecase

import "context"

type AnimeReadRepository interface {
	ListAnime(ctx context.Context, userID int64) ([]AnimeListItem, error)
	GetStats(ctx context.Context, userID int64) (AnimeStats, error)
}

type AnimeQueryService struct {
	repo AnimeReadRepository
}

func NewAnimeQueryService(repo AnimeReadRepository) *AnimeQueryService {
	return &AnimeQueryService{repo: repo}
}

func (service *AnimeQueryService) ListAnime(ctx context.Context, userID int64) ([]AnimeListItem, error) {
	return service.repo.ListAnime(ensureContext(ctx), userID)
}

func (service *AnimeQueryService) GetStats(ctx context.Context, userID int64) (AnimeStats, error) {
	return service.repo.GetStats(ensureContext(ctx), userID)
}
