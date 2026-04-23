package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

const (
	syncJobModeSession = "session"
	syncJobModePublic  = "public"

	syncJobStatusQueued    = "queued"
	syncJobStatusRunning   = "running"
	syncJobStatusCompleted = "completed"
	syncJobStatusFailed    = "failed"

	syncJobPhaseQueued           = "queued"
	syncJobPhaseFetchingList     = "fetching_list"
	syncJobPhaseListFetched      = "list_fetched"
	syncJobPhaseSavingSnapshot   = "saving_snapshot"
	syncJobPhaseHydratingCatalog = "hydrating_catalog"
	syncJobPhaseGrouping         = "grouping"
	syncJobPhaseDone             = "done"

	syncJobRetention              = 30 * time.Minute
	syncJobProgressUpdateInterval = 2 * time.Second
)

type SyncJobSnapshot struct {
	ID         string     `json:"id"`
	UserID     int64      `json:"-"`
	Mode       string     `json:"mode"`
	Username   string     `json:"username"`
	Status     string     `json:"status"`
	Phase      string     `json:"phase"`
	Current    int        `json:"current"`
	Total      int        `json:"total"`
	Message    string     `json:"message"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type SyncJob struct {
	mu                 sync.Mutex
	snapshot           SyncJobSnapshot
	subscribers        map[chan SyncJobSnapshot]struct{}
	lastProgressUpdate time.Time
}

func newSyncJob(id string, userID int64, username, mode string) *SyncJob {
	return &SyncJob{
		snapshot: SyncJobSnapshot{
			ID:        id,
			UserID:    userID,
			Mode:      strings.TrimSpace(mode),
			Username:  strings.TrimSpace(username),
			Status:    syncJobStatusQueued,
			Phase:     syncJobPhaseQueued,
			Message:   "Sync queued",
			StartedAt: time.Now().UTC(),
		},
		subscribers: make(map[chan SyncJobSnapshot]struct{}),
	}
}

func (job *SyncJob) Snapshot() SyncJobSnapshot {
	if job == nil {
		return SyncJobSnapshot{}
	}

	job.mu.Lock()
	defer job.mu.Unlock()
	return cloneSyncJobSnapshot(job.snapshot)
}

func (job *SyncJob) Start(message string) {
	job.Update(syncJobPhaseFetchingList, 0, 0, message)
}

func (job *SyncJob) Update(phase string, current, total int, message string) {
	if job == nil {
		return
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	job.applyUpdateLocked(phase, current, total, message)
	job.broadcastLocked(cloneSyncJobSnapshot(job.snapshot))
}

func (job *SyncJob) UpdateThrottled(phase string, current, total int, message string, interval time.Duration) {
	if job == nil {
		return
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	now := time.Now()
	isFinalProgress := total > 0 && current >= total
	if !isFinalProgress && !job.lastProgressUpdate.IsZero() && now.Sub(job.lastProgressUpdate) < interval {
		return
	}

	job.applyUpdateLocked(phase, current, total, message)
	job.lastProgressUpdate = now
	job.broadcastLocked(cloneSyncJobSnapshot(job.snapshot))
}

func (job *SyncJob) Complete(message string) {
	if job == nil {
		return
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	finishedAt := time.Now().UTC()
	job.snapshot.Status = syncJobStatusCompleted
	job.snapshot.Phase = syncJobPhaseDone
	if job.snapshot.Total > 0 {
		job.snapshot.Current = job.snapshot.Total
	}
	job.snapshot.Message = firstNonEmpty(strings.TrimSpace(message), "Sync completed")
	job.snapshot.Error = ""
	job.snapshot.FinishedAt = &finishedAt
	job.broadcastLocked(cloneSyncJobSnapshot(job.snapshot))
}

func (job *SyncJob) Fail(err error) {
	if job == nil {
		return
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	finishedAt := time.Now().UTC()
	job.snapshot.Status = syncJobStatusFailed
	if job.snapshot.Phase == "" || job.snapshot.Phase == syncJobPhaseQueued {
		job.snapshot.Phase = syncJobPhaseDone
	}
	job.snapshot.Message = "Sync failed"
	if err != nil {
		job.snapshot.Error = err.Error()
	}
	job.snapshot.FinishedAt = &finishedAt
	job.broadcastLocked(cloneSyncJobSnapshot(job.snapshot))
}

func (job *SyncJob) Subscribe() (<-chan SyncJobSnapshot, func()) {
	if job == nil {
		ch := make(chan SyncJobSnapshot)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan SyncJobSnapshot, 1)

	job.mu.Lock()
	job.subscribers[ch] = struct{}{}
	snapshot := cloneSyncJobSnapshot(job.snapshot)
	ch <- snapshot
	job.mu.Unlock()

	unsubscribe := func() {
		job.mu.Lock()
		if _, exists := job.subscribers[ch]; exists {
			delete(job.subscribers, ch)
			close(ch)
		}
		job.mu.Unlock()
	}

	return ch, unsubscribe
}

func (job *SyncJob) applyUpdateLocked(phase string, current, total int, message string) {
	job.snapshot.Status = syncJobStatusRunning
	job.snapshot.Phase = strings.TrimSpace(phase)
	job.snapshot.Current = clampProgressValue(current)
	job.snapshot.Total = clampProgressValue(total)
	if job.snapshot.Total > 0 && job.snapshot.Current > job.snapshot.Total {
		job.snapshot.Current = job.snapshot.Total
	}
	if strings.TrimSpace(message) != "" {
		job.snapshot.Message = strings.TrimSpace(message)
	}
	job.snapshot.Error = ""
}

func (job *SyncJob) broadcastLocked(snapshot SyncJobSnapshot) {
	for subscriber := range job.subscribers {
		select {
		case subscriber <- snapshot:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- snapshot:
			default:
			}
		}
	}
}

func cloneSyncJobSnapshot(snapshot SyncJobSnapshot) SyncJobSnapshot {
	if snapshot.FinishedAt != nil {
		finishedAt := *snapshot.FinishedAt
		snapshot.FinishedAt = &finishedAt
	}
	return snapshot
}

func clampProgressValue(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func syncJobIsFinal(status string) bool {
	return status == syncJobStatusCompleted || status == syncJobStatusFailed
}

func (a *App) createSyncJob(userID int64, username, mode string) (*SyncJob, error) {
	a.syncJobsMu.Lock()
	defer a.syncJobsMu.Unlock()

	if a.syncJobs == nil {
		a.syncJobs = make(map[string]*SyncJob)
	}
	a.pruneOldSyncJobsLocked(time.Now())

	for attempt := 0; attempt < 5; attempt++ {
		id, err := randomURLSafe(24)
		if err != nil {
			return nil, err
		}
		if _, exists := a.syncJobs[id]; exists {
			continue
		}

		job := newSyncJob(id, userID, username, mode)
		a.syncJobs[id] = job
		return job, nil
	}

	return nil, errors.New("could not allocate unique sync job id")
}

func (a *App) syncJobByID(jobID string) (*SyncJob, bool) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, false
	}

	a.syncJobsMu.Lock()
	defer a.syncJobsMu.Unlock()

	job, exists := a.syncJobs[jobID]
	return job, exists
}

func (a *App) pruneOldSyncJobsLocked(now time.Time) {
	for id, job := range a.syncJobs {
		snapshot := job.Snapshot()
		if snapshot.FinishedAt == nil {
			continue
		}
		if now.Sub(*snapshot.FinishedAt) > syncJobRetention {
			delete(a.syncJobs, id)
		}
	}
}

func (a *App) syncJobFromRequest(r *http.Request) (*SyncJob, error) {
	jobID := strings.TrimSpace(mux.Vars(r)["job_id"])
	if jobID == "" {
		return nil, errors.New("job_id is required")
	}

	job, exists := a.syncJobByID(jobID)
	if !exists {
		return nil, ErrSyncJobNotFound
	}
	return job, nil
}

var ErrSyncJobNotFound = errors.New("sync job not found")

func writeSSESnapshot(w http.ResponseWriter, snapshot SyncJobSnapshot) error {
	body, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
}
