package httpapi

import (
	"errors"
	"strings"
	"sync"
	"time"

	"test/internal/domain"
	"test/internal/ports"
)

const (
	syncJobModeSession = "session"

	syncJobStatusQueued    = domain.SyncJobStatusQueued
	syncJobStatusRunning   = domain.SyncJobStatusRunning
	syncJobStatusCompleted = domain.SyncJobStatusCompleted
	syncJobStatusFailed    = domain.SyncJobStatusFailed

	syncJobPhaseQueued = ports.SyncJobPhaseQueued
	syncJobPhaseDone   = ports.SyncJobPhaseDone

	syncJobRetention = 30 * time.Minute
)

type syncJobProgressSnapshot struct {
	ID         string
	UserID     int64
	Mode       string
	Username   string
	Status     string
	Phase      ports.SyncProgressPhase
	Current    int
	Total      int
	Message    string
	Error      string
	StartedAt  time.Time
	FinishedAt *time.Time
}

type SyncJob struct {
	mu                 sync.Mutex
	snapshot           syncJobProgressSnapshot
	subscribers        map[chan syncJobProgressSnapshot]struct{}
	lastProgressUpdate time.Time
}

var _ ports.SyncProgressReporter = (*SyncJob)(nil)

type SyncJobStore interface {
	Create(userID int64, username, mode string) (*SyncJob, error)
	Find(jobID string) (*SyncJob, bool)
}

type InMemorySyncJobStore struct {
	mu   sync.Mutex
	jobs map[string]*SyncJob
}

func NewInMemorySyncJobStore() *InMemorySyncJobStore {
	return &InMemorySyncJobStore{
		jobs: make(map[string]*SyncJob),
	}
}

func newSyncJob(id string, userID int64, username, mode string) *SyncJob {
	return &SyncJob{
		snapshot: syncJobProgressSnapshot{
			ID:        id,
			UserID:    userID,
			Mode:      strings.TrimSpace(mode),
			Username:  strings.TrimSpace(username),
			Status:    syncJobStatusQueued,
			Phase:     syncJobPhaseQueued,
			Message:   "Sync queued",
			StartedAt: time.Now().UTC(),
		},
		subscribers: make(map[chan syncJobProgressSnapshot]struct{}),
	}
}

func (job *SyncJob) snapshotCopy() syncJobProgressSnapshot {
	if job == nil {
		return syncJobProgressSnapshot{}
	}

	job.mu.Lock()
	defer job.mu.Unlock()
	return cloneSyncJobProgressSnapshot(job.snapshot)
}

func (job *SyncJob) Start(message string) {
	job.Update(ports.SyncJobPhaseFetchingList, 0, 0, message)
}

func (job *SyncJob) Update(phase ports.SyncProgressPhase, current, total int, message string) {
	if job == nil {
		return
	}

	job.mu.Lock()
	defer job.mu.Unlock()

	job.applyUpdateLocked(phase, current, total, message)
	job.broadcastLocked(cloneSyncJobProgressSnapshot(job.snapshot))
}

func (job *SyncJob) UpdateThrottled(phase ports.SyncProgressPhase, current, total int, message string, interval time.Duration) {
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
	job.broadcastLocked(cloneSyncJobProgressSnapshot(job.snapshot))
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
	job.broadcastLocked(cloneSyncJobProgressSnapshot(job.snapshot))
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
	job.broadcastLocked(cloneSyncJobProgressSnapshot(job.snapshot))
}

func (job *SyncJob) subscribe() (<-chan syncJobProgressSnapshot, func()) {
	if job == nil {
		ch := make(chan syncJobProgressSnapshot)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan syncJobProgressSnapshot, 1)

	job.mu.Lock()
	job.subscribers[ch] = struct{}{}
	snapshot := cloneSyncJobProgressSnapshot(job.snapshot)
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

func (job *SyncJob) applyUpdateLocked(phase ports.SyncProgressPhase, current, total int, message string) {
	job.snapshot.Status = syncJobStatusRunning
	job.snapshot.Phase = ports.SyncProgressPhase(strings.TrimSpace(string(phase)))
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

func (job *SyncJob) broadcastLocked(snapshot syncJobProgressSnapshot) {
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

func cloneSyncJobProgressSnapshot(snapshot syncJobProgressSnapshot) syncJobProgressSnapshot {
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

func syncJobProgressIsFinal(snapshot syncJobProgressSnapshot) bool {
	return snapshot.Status == syncJobStatusCompleted || snapshot.Status == syncJobStatusFailed
}

func (store *InMemorySyncJobStore) Create(userID int64, username, mode string) (*SyncJob, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

	if store.jobs == nil {
		store.jobs = make(map[string]*SyncJob)
	}
	store.pruneOldSyncJobsLocked(time.Now())

	for attempt := 0; attempt < 5; attempt++ {
		id, err := randomURLSafe(24)
		if err != nil {
			return nil, err
		}
		if _, exists := store.jobs[id]; exists {
			continue
		}

		job := newSyncJob(id, userID, username, mode)
		store.jobs[id] = job
		return job, nil
	}

	return nil, errors.New("could not allocate unique sync job id")
}

func (store *InMemorySyncJobStore) Find(jobID string) (*SyncJob, bool) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, false
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	job, exists := store.jobs[jobID]
	return job, exists
}

func (store *InMemorySyncJobStore) pruneOldSyncJobsLocked(now time.Time) {
	for id, job := range store.jobs {
		snapshot := job.snapshotCopy()
		if snapshot.FinishedAt == nil {
			continue
		}
		if now.Sub(*snapshot.FinishedAt) > syncJobRetention {
			delete(store.jobs, id)
		}
	}
}
