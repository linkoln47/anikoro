package usecase

import (
	"context"

	"test/internal/domain"
	"test/internal/ports"
)

// FranchiseQueryService serves the public franchise view. It reads the global
// franchise grouping from the catalog with no user-list data, so any anime id
// resolves the same way regardless of session.
type FranchiseQueryService struct {
	repo ports.FranchiseReadRepository
}

func NewFranchiseQueryService(repo ports.FranchiseReadRepository) *FranchiseQueryService {
	return &FranchiseQueryService{repo: repo}
}

func (service *FranchiseQueryService) GetFranchise(ctx context.Context, animeID int) (domain.AnimeListItem, bool, error) {
	return service.repo.GetFranchise(ensureContext(ctx), animeID)
}
