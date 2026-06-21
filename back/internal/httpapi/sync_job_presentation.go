package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"test/internal/ports"
)

type SyncJobResponse struct {
	ID         string                  `json:"id"`
	Mode       string                  `json:"mode"`
	Username   string                  `json:"username"`
	Status     string                  `json:"status"`
	Phase      ports.SyncProgressPhase `json:"phase"`
	Current    int                     `json:"current"`
	Total      int                     `json:"total"`
	Message    string                  `json:"message"`
	Error      string                  `json:"error,omitempty"`
	StartedAt  time.Time               `json:"started_at"`
	FinishedAt *time.Time              `json:"finished_at,omitempty"`
}

type PublicSyncJobResponse struct {
	ID      string                  `json:"id"`
	Status  string                  `json:"status"`
	Phase   ports.SyncProgressPhase `json:"phase"`
	Current int                     `json:"current"`
	Total   int                     `json:"total"`
	Message string                  `json:"message"`
}

type syncJobResponseScope int

const (
	syncJobResponseScopeOwner syncJobResponseScope = iota
	syncJobResponseScopePublic
)

func newSyncJobResponse(snapshot syncJobProgressSnapshot, scope syncJobResponseScope) any {
	if scope == syncJobResponseScopePublic {
		return PublicSyncJobResponse{
			ID:      snapshot.ID,
			Status:  snapshot.Status,
			Phase:   snapshot.Phase,
			Current: snapshot.Current,
			Total:   snapshot.Total,
			Message: publicSyncJobMessage(snapshot),
		}
	}

	return SyncJobResponse{
		ID:         snapshot.ID,
		Mode:       snapshot.Mode,
		Username:   snapshot.Username,
		Status:     snapshot.Status,
		Phase:      snapshot.Phase,
		Current:    snapshot.Current,
		Total:      snapshot.Total,
		Message:    snapshot.Message,
		Error:      snapshot.Error,
		StartedAt:  snapshot.StartedAt,
		FinishedAt: snapshot.FinishedAt,
	}
}

func publicSyncJobMessage(snapshot syncJobProgressSnapshot) string {
	if snapshot.Status == syncJobStatusFailed {
		return "Public sync failed. Try again later."
	}
	return snapshot.Message
}

func (api *HTTPAPI) syncJobFromRequest(r *http.Request) (*SyncJob, syncJobResponseScope, error) {
	jobID := strings.TrimSpace(mux.Vars(r)["job_id"])
	if jobID == "" {
		return nil, syncJobResponseScopePublic, errors.New("job_id is required")
	}

	job, exists := api.syncJobs.Find(jobID)
	if !exists {
		return nil, syncJobResponseScopePublic, ErrSyncJobNotFound
	}

	snapshot := job.snapshotCopy()
	switch snapshot.Mode {
	case syncJobModeSession:
		user, err := api.currentUserFromRequest(r)
		if err != nil {
			return nil, syncJobResponseScopeOwner, err
		}
		if user.ID != snapshot.UserID {
			return nil, syncJobResponseScopeOwner, ErrSyncJobForbidden
		}
		return job, syncJobResponseScopeOwner, nil
	default:
		return nil, syncJobResponseScopeOwner, ErrSyncJobForbidden
	}
}

var ErrSyncJobNotFound = errors.New("sync job not found")
var ErrSyncJobForbidden = errors.New("sync job access denied")

func writeSSESnapshot(w http.ResponseWriter, snapshot syncJobProgressSnapshot, scope syncJobResponseScope) error {
	body, err := json.Marshal(newSyncJobResponse(snapshot, scope))
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
}
