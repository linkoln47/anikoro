package usecase

import (
	"context"
	"time"

	"test/internal/domain"
)

const (
	SyncJobPhaseFetchingList     = domain.SyncJobPhaseFetchingList
	SyncJobPhaseListFetched      = domain.SyncJobPhaseListFetched
	SyncJobPhaseSavingSnapshot   = domain.SyncJobPhaseSavingSnapshot
	SyncJobPhaseHydratingCatalog = domain.SyncJobPhaseHydratingCatalog
	SyncJobPhaseGrouping         = domain.SyncJobPhaseGrouping
	SyncJobPhaseDone             = domain.SyncJobPhaseDone

	SyncJobProgressUpdateInterval = 2 * time.Second
)

func ensureContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func uniquePositiveIDs(ids []int) []int {
	unique := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	return unique
}
