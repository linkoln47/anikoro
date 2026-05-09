package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestSyncJobFromRequestRequiresSessionOwner(t *testing.T) {
	store := NewInMemorySyncJobStore()
	api := New(Dependencies{
		Config: Config{
			SessionSecret: "test-session-secret",
		},
		SyncJobs: store,
	})

	job, err := store.Create(42, "owner", syncJobModeSession)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	jobID := job.snapshotCopy().ID

	_, _, err = api.syncJobFromRequest(syncJobRequest(jobID))
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expected unauthenticated error without session, got %v", err)
	}

	otherUserRequest := syncJobRequest(jobID)
	addSessionCookie(t, api, otherUserRequest, 7, "other")
	_, _, err = api.syncJobFromRequest(otherUserRequest)
	if !errors.Is(err, ErrSyncJobForbidden) {
		t.Fatalf("expected forbidden error for another user, got %v", err)
	}

	ownerRequest := syncJobRequest(jobID)
	addSessionCookie(t, api, ownerRequest, 42, "owner")
	gotJob, scope, err := api.syncJobFromRequest(ownerRequest)
	if err != nil {
		t.Fatalf("expected owner to access session job, got %v", err)
	}
	if gotJob != job {
		t.Fatalf("expected original job")
	}
	if scope != syncJobResponseScopeOwner {
		t.Fatalf("expected owner scope, got %v", scope)
	}
}

func TestSyncJobFromRequestAllowsPublicJobWithoutSession(t *testing.T) {
	store := NewInMemorySyncJobStore()
	api := New(Dependencies{SyncJobs: store})

	job, err := store.Create(42, "public-user", syncJobModePublic)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	gotJob, scope, err := api.syncJobFromRequest(syncJobRequest(job.snapshotCopy().ID))
	if err != nil {
		t.Fatalf("expected public job to be readable without session, got %v", err)
	}
	if gotJob != job {
		t.Fatalf("expected original job")
	}
	if scope != syncJobResponseScopePublic {
		t.Fatalf("expected public scope, got %v", scope)
	}
}

func TestPublicSyncJobResponseRedactsInternalFields(t *testing.T) {
	body, err := json.Marshal(newSyncJobResponse(syncJobProgressSnapshot{
		ID:       "job-1",
		UserID:   42,
		Mode:     syncJobModePublic,
		Username: "public-user",
		Status:   syncJobStatusFailed,
		Phase:    syncJobPhaseDone,
		Message:  "Sync failed",
		Error:    "database connection failed with internal details",
	}, syncJobResponseScopePublic))
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	response := string(body)
	for _, sensitive := range []string{"mode", "username", "error", "public-user", "database connection"} {
		if strings.Contains(response, sensitive) {
			t.Fatalf("public response leaked %q: %s", sensitive, response)
		}
	}
	if !strings.Contains(response, "Public sync failed. Try again later.") {
		t.Fatalf("expected sanitized public failure message, got %s", response)
	}
}

func syncJobRequest(jobID string) *http.Request {
	request := httptest.NewRequest("GET", "/api/sync/jobs/"+jobID, nil)
	return mux.SetURLVars(request, map[string]string{"job_id": jobID})
}

func addSessionCookie(t *testing.T, api *HTTPAPI, request *http.Request, userID int64, username string) {
	t.Helper()

	cookieValue, err := api.signCookiePayload(signedSessionPayload{
		UserID:    userID,
		Username:  username,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("signCookiePayload returned error: %v", err)
	}

	request.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: cookieValue,
	})
}
