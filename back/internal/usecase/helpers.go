package usecase

import (
	"context"
	"strings"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

const (
	DetailsCacheTTL = ports.DetailsCacheTTL

	SyncJobPhaseFetchingList     = domain.SyncJobPhaseFetchingList
	SyncJobPhaseListFetched      = domain.SyncJobPhaseListFetched
	SyncJobPhaseSavingSnapshot   = domain.SyncJobPhaseSavingSnapshot
	SyncJobPhaseHydratingCatalog = domain.SyncJobPhaseHydratingCatalog
	SyncJobPhaseGrouping         = domain.SyncJobPhaseGrouping
	SyncJobPhaseDone             = domain.SyncJobPhaseDone

	SyncJobProgressUpdateInterval = 2 * time.Second
)

func bearerMALAuth(token string) ports.MALAuth {
	return ports.MALAuth{BearerToken: strings.TrimSpace(token)}
}

func clientIDMALAuth(clientID string) ports.MALAuth {
	return ports.MALAuth{ClientID: strings.TrimSpace(clientID)}
}

type noopSyncProgressReporter struct{}

func (noopSyncProgressReporter) Start(string)                                            {}
func (noopSyncProgressReporter) Update(string, int, int, string)                         {}
func (noopSyncProgressReporter) UpdateThrottled(string, int, int, string, time.Duration) {}
func (noopSyncProgressReporter) Complete(string)                                         {}
func (noopSyncProgressReporter) Fail(error)                                              {}

func ensureSyncProgressReporter(reporter ports.SyncProgressReporter) ports.SyncProgressReporter {
	if reporter == nil {
		return noopSyncProgressReporter{}
	}
	return reporter
}

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
