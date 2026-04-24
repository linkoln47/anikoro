package main

import (
	"context"
	"net/http"
	"reflect"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestSyncHelpers_SyncAnimeWithContext_ClearsSnapshotWhenCompletedListIsEmpty(t *testing.T) {
	sut, mock := newInternalTestApp(t)
	sut.HTTPClient.Transport = internalFakeTransport{
		roundTrip: func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Authorization"); got != "Bearer token" {
				t.Fatalf("authorization header = %q, want %q", got, "Bearer token")
			}
			return internalJSONHTTPResponse(http.StatusOK, `{"data":[],"paging":{"next":""}}`), nil
		},
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT set_config('app.user_id', $1, true)")).
		WithArgs("42").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM user_anime_items WHERE user_id = $1")).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	if err := sut.syncAnimeWithContext(context.Background(), 42, "token"); err != nil {
		t.Fatalf("syncAnimeWithContext returned error: %v", err)
	}
}

func TestSyncHelpers_SummarizeRetryErrors(t *testing.T) {
	got := summarizeRetryErrors([]string{"a", "b", "c", "d"})
	if got != "a; b; c; and 1 more" {
		t.Fatalf("summarizeRetryErrors = %q, want %q", got, "a; b; c; and 1 more")
	}
}

func TestSyncHelpers_SortedMemberIDs(t *testing.T) {
	got, err := sortedMemberIDs(map[int]struct{}{
		30: {},
		10: {},
		0:  {},
		-1: {},
	})
	if err != nil {
		t.Fatalf("sortedMemberIDs returned error: %v", err)
	}

	want := []int{10, 30}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted ids mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	_, err = sortedMemberIDs(map[int]struct{}{
		0:  {},
		-5: {},
	})
	if err == nil {
		t.Fatal("expected error for map without positive ids")
	}
}

func TestSyncHelpers_BuildGroupKey(t *testing.T) {
	if got := buildGroupKey([]int{2, 10, 30}); got != "2:10:30" {
		t.Fatalf("group key = %q, want %q", got, "2:10:30")
	}
}

func TestSyncHelpers_SortGroupedViews(t *testing.T) {
	groups := []GroupedView{
		{DisplayTitle: "Bravo", WatchedEpisodesSum: 10},
		{DisplayTitle: "Alpha", WatchedEpisodesSum: 10},
		{DisplayTitle: "Charlie", WatchedEpisodesSum: 12},
	}

	sortGroupedViews(groups)

	want := []GroupedView{
		{DisplayTitle: "Charlie", WatchedEpisodesSum: 12},
		{DisplayTitle: "Alpha", WatchedEpisodesSum: 10},
		{DisplayTitle: "Bravo", WatchedEpisodesSum: 10},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("sorted grouped views mismatch:\n got: %#v\nwant: %#v", groups, want)
	}
}
