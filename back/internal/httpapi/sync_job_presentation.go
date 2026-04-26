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

func newSyncJobResponse(snapshot syncJobProgressSnapshot) SyncJobResponse {
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

func (api *HTTPAPI) syncJobFromRequest(r *http.Request) (*SyncJob, error) {
	jobID := strings.TrimSpace(mux.Vars(r)["job_id"])
	if jobID == "" {
		return nil, errors.New("job_id is required")
	}

	job, exists := api.syncJobs.Find(jobID)
	if !exists {
		return nil, ErrSyncJobNotFound
	}
	return job, nil
}

var ErrSyncJobNotFound = errors.New("sync job not found")

func writeSSESnapshot(w http.ResponseWriter, snapshot syncJobProgressSnapshot) error {
	body, err := json.Marshal(newSyncJobResponse(snapshot))
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(w, "data: %s\n\n", body)
	return err
}
