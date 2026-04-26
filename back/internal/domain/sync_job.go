package domain

const (
	SyncJobStatusQueued    = "queued"
	SyncJobStatusRunning   = "running"
	SyncJobStatusCompleted = "completed"
	SyncJobStatusFailed    = "failed"

	SyncJobPhaseQueued           = "queued"
	SyncJobPhaseFetchingList     = "fetching_list"
	SyncJobPhaseListFetched      = "list_fetched"
	SyncJobPhaseSavingSnapshot   = "saving_snapshot"
	SyncJobPhaseHydratingCatalog = "hydrating_catalog"
	SyncJobPhaseGrouping         = "grouping"
	SyncJobPhaseDone             = "done"
)
