package usecase

import (
	"context"
	"time"
)

const (
	SyncJobPhaseFetchingList     = "fetching_list"
	SyncJobPhaseListFetched      = "list_fetched"
	SyncJobPhaseSavingSnapshot   = "saving_snapshot"
	SyncJobPhaseHydratingCatalog = "hydrating_catalog"
	SyncJobPhaseGrouping         = "grouping"
	SyncJobPhaseDone             = "done"

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
