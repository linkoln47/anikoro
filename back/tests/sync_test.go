package tests

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	backend "test/internal/app"
)

func TestGroupCompletedAnimeEntriesWithResolvers_GroupsRelatedEntriesAndSplitsMovies(t *testing.T) {
	sut, _ := newTestApp(t)
	cache := backend.NewAnimeDetailsCacheStore(sut, nil, backend.DetailsCacheFlushBatch)

	entries := []backend.AnimeEntry{
		{ID: 30, Title: "Standalone Movie", Score: 10, NumEpisodesWatched: 1},
		{ID: 20, Title: "Series Season 2", Score: 8, NumEpisodesWatched: 12},
		{ID: 10, Title: "Series Season 1", Score: 9, NumEpisodesWatched: 12},
	}

	detailsByID := map[int]backend.AnimeDetailsInfo{
		10: {RelatedIDs: []int{20}, MediaType: "tv"},
		20: {RelatedIDs: []int{10}, MediaType: "tv"},
		30: {RelatedIDs: []int{999}, MediaType: "movie"},
	}

	primaryResolver := func(_ string, animeID int, _ *backend.AnimeDetailsCacheStore) (backend.AnimeDetailsInfo, error) {
		return detailsByID[animeID], nil
	}
	retryResolver := func(_ string, animeID int) (backend.AnimeDetailsInfo, error) {
		return backend.AnimeDetailsInfo{}, errors.New("unexpected retry for id=" + backend.BuildGroupKey([]int{animeID}))
	}

	seriesGroups, movieGroups, err := sut.GroupCompletedAnimeEntriesWithResolvers("token", entries, cache, primaryResolver, retryResolver)
	if err != nil {
		t.Fatalf("GroupCompletedAnimeEntriesWithResolvers returned error: %v", err)
	}

	wantSeries := []backend.GroupedView{
		{
			ID:                 10,
			GroupKey:           "10:20",
			DisplayTitle:       "Series Season 2",
			MergedTitles:       2,
			AvgScore:           8.5,
			WatchedEpisodesSum: 24,
		},
	}
	if !reflect.DeepEqual(seriesGroups, wantSeries) {
		t.Fatalf("series groups mismatch:\n got: %#v\nwant: %#v", seriesGroups, wantSeries)
	}

	wantMovies := []backend.GroupedView{
		{
			ID:                 30,
			GroupKey:           "30",
			DisplayTitle:       "Standalone Movie",
			MergedTitles:       1,
			AvgScore:           10,
			WatchedEpisodesSum: 1,
		},
	}
	if !reflect.DeepEqual(movieGroups, wantMovies) {
		t.Fatalf("movie groups mismatch:\n got: %#v\nwant: %#v", movieGroups, wantMovies)
	}
}

func TestGroupCompletedAnimeEntriesWithResolvers_LeavesLinkedMovieInSeries(t *testing.T) {
	sut, _ := newTestApp(t)
	cache := backend.NewAnimeDetailsCacheStore(sut, nil, backend.DetailsCacheFlushBatch)

	entries := []backend.AnimeEntry{
		{ID: 100, Title: "Movie Part", Score: 7, NumEpisodesWatched: 1},
		{ID: 200, Title: "TV Follow-up", Score: 8, NumEpisodesWatched: 3},
	}

	detailsByID := map[int]backend.AnimeDetailsInfo{
		100: {RelatedIDs: []int{200}, MediaType: "movie"},
		200: {RelatedIDs: []int{100}, MediaType: "tv"},
	}

	primaryResolver := func(_ string, animeID int, _ *backend.AnimeDetailsCacheStore) (backend.AnimeDetailsInfo, error) {
		return detailsByID[animeID], nil
	}
	retryResolver := func(_ string, animeID int) (backend.AnimeDetailsInfo, error) {
		return backend.AnimeDetailsInfo{}, errors.New("unexpected retry for id=" + backend.BuildGroupKey([]int{animeID}))
	}

	seriesGroups, movieGroups, err := sut.GroupCompletedAnimeEntriesWithResolvers("token", entries, cache, primaryResolver, retryResolver)
	if err != nil {
		t.Fatalf("GroupCompletedAnimeEntriesWithResolvers returned error: %v", err)
	}

	if len(movieGroups) != 0 {
		t.Fatalf("expected no movie groups, got %#v", movieGroups)
	}

	wantSeries := []backend.GroupedView{
		{
			ID:                 100,
			GroupKey:           "100:200",
			DisplayTitle:       "Movie Part",
			MergedTitles:       2,
			AvgScore:           7.5,
			WatchedEpisodesSum: 4,
		},
	}
	if !reflect.DeepEqual(seriesGroups, wantSeries) {
		t.Fatalf("series groups mismatch:\n got: %#v\nwant: %#v", seriesGroups, wantSeries)
	}
}

func TestGroupCompletedAnimeEntriesWithResolvers_UsesRetryResolverAndCachesResult(t *testing.T) {
	sut, _ := newTestApp(t)
	cache := backend.NewAnimeDetailsCacheStore(sut, nil, 1000)

	entries := []backend.AnimeEntry{
		{ID: 7, Title: "Needs Retry", Score: 6, NumEpisodesWatched: 1},
	}

	primaryCalls := 0
	retryCalls := 0

	primaryResolver := func(_ string, animeID int, _ *backend.AnimeDetailsCacheStore) (backend.AnimeDetailsInfo, error) {
		primaryCalls++
		return backend.AnimeDetailsInfo{}, errors.New("primary lookup failed")
	}
	retryResolver := func(_ string, animeID int) (backend.AnimeDetailsInfo, error) {
		retryCalls++
		return backend.AnimeDetailsInfo{MediaType: "movie"}, nil
	}

	seriesGroups, movieGroups, err := sut.GroupCompletedAnimeEntriesWithResolvers("token", entries, cache, primaryResolver, retryResolver)
	if err != nil {
		t.Fatalf("GroupCompletedAnimeEntriesWithResolvers returned error: %v", err)
	}

	if primaryCalls != 1 {
		t.Fatalf("primary resolver calls = %d, want 1", primaryCalls)
	}
	if retryCalls != 1 {
		t.Fatalf("retry resolver calls = %d, want 1", retryCalls)
	}
	if len(seriesGroups) != 0 {
		t.Fatalf("expected no series groups, got %#v", seriesGroups)
	}

	wantMovies := []backend.GroupedView{
		{
			ID:                 7,
			GroupKey:           "7",
			DisplayTitle:       "Needs Retry",
			MergedTitles:       1,
			AvgScore:           6,
			WatchedEpisodesSum: 1,
		},
	}
	if !reflect.DeepEqual(movieGroups, wantMovies) {
		t.Fatalf("movie groups mismatch:\n got: %#v\nwant: %#v", movieGroups, wantMovies)
	}

	cached, ok := cache.Lookup(7)
	if !ok {
		t.Fatal("expected retry result to be cached")
	}
	if !cached.Resolved {
		t.Fatal("expected cached retry result to be marked resolved")
	}
	if cached.MediaType != "movie" {
		t.Fatalf("cached media type = %q, want %q", cached.MediaType, "movie")
	}
}

func TestGroupCompletedAnimeEntriesWithResolvers_ReturnsSummarizedRetryErrors(t *testing.T) {
	sut, _ := newTestApp(t)
	cache := backend.NewAnimeDetailsCacheStore(sut, nil, backend.DetailsCacheFlushBatch)

	entries := []backend.AnimeEntry{
		{ID: 1, Title: "One"},
		{ID: 2, Title: "Two"},
		{ID: 3, Title: "Three"},
		{ID: 4, Title: "Four"},
	}

	primaryResolver := func(_ string, animeID int, _ *backend.AnimeDetailsCacheStore) (backend.AnimeDetailsInfo, error) {
		return backend.AnimeDetailsInfo{}, errors.New("primary lookup failed")
	}
	retryResolver := func(_ string, animeID int) (backend.AnimeDetailsInfo, error) {
		return backend.AnimeDetailsInfo{}, errors.New("retry lookup failed")
	}

	_, _, err := sut.GroupCompletedAnimeEntriesWithResolvers("token", entries, cache, primaryResolver, retryResolver)
	if err == nil {
		t.Fatal("expected retry failure error, got nil")
	}

	message := err.Error()
	if !strings.Contains(message, "failed to resolve anime details after retry for 4 entries") {
		t.Fatalf("error %q does not mention retry failure count", message)
	}
	if !strings.Contains(message, "retry lookup failed") {
		t.Fatalf("error %q does not include retry failure details", message)
	}
	if !strings.Contains(message, "and 1 more") {
		t.Fatalf("error %q does not include summarized tail", message)
	}
}

func TestGroupCompletedAnimeEntriesWithResolvers_StartsRetryWhilePrimaryStillRunning(t *testing.T) {
	sut, _ := newTestApp(t)
	cache := backend.NewAnimeDetailsCacheStore(sut, nil, 1000)

	entries := []backend.AnimeEntry{
		{ID: 1, Title: "Retry First", Score: 7, NumEpisodesWatched: 1},
		{ID: 2, Title: "Slow Primary", Score: 8, NumEpisodesWatched: 12},
	}

	slowPrimaryRelease := make(chan struct{})
	retryStarted := make(chan struct{}, 1)

	primaryResolver := func(_ string, animeID int, _ *backend.AnimeDetailsCacheStore) (backend.AnimeDetailsInfo, error) {
		switch animeID {
		case 1:
			return backend.AnimeDetailsInfo{}, errors.New("primary lookup failed")
		case 2:
			<-slowPrimaryRelease
			return backend.AnimeDetailsInfo{MediaType: "tv"}, nil
		default:
			return backend.AnimeDetailsInfo{}, errors.New("unexpected anime id")
		}
	}
	retryResolver := func(_ string, animeID int) (backend.AnimeDetailsInfo, error) {
		if animeID == 1 {
			select {
			case retryStarted <- struct{}{}:
			default:
			}
			return backend.AnimeDetailsInfo{MediaType: "movie"}, nil
		}
		return backend.AnimeDetailsInfo{}, errors.New("unexpected retry anime id")
	}

	done := make(chan error, 1)
	go func() {
		_, _, err := sut.GroupCompletedAnimeEntriesWithResolvers("token", entries, cache, primaryResolver, retryResolver)
		done <- err
	}()

	select {
	case <-retryStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("retry did not start while another primary request was still running")
	}

	close(slowPrimaryRelease)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("GroupCompletedAnimeEntriesWithResolvers returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("GroupCompletedAnimeEntriesWithResolvers did not finish after releasing slow primary")
	}
}

func TestSortedMemberIDs(t *testing.T) {
	got, err := backend.SortedMemberIDs(map[int]struct{}{
		30: {},
		10: {},
		0:  {},
		-1: {},
	})
	if err != nil {
		t.Fatalf("SortedMemberIDs returned error: %v", err)
	}

	want := []int{10, 30}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted ids mismatch:\n got: %#v\nwant: %#v", got, want)
	}

	_, err = backend.SortedMemberIDs(map[int]struct{}{
		0:  {},
		-5: {},
	})
	if err == nil {
		t.Fatal("expected error for map without positive ids")
	}
}

func TestBuildGroupKey(t *testing.T) {
	if got := backend.BuildGroupKey([]int{2, 10, 30}); got != "2:10:30" {
		t.Fatalf("group key = %q, want %q", got, "2:10:30")
	}
}

func TestSortGroupedViews(t *testing.T) {
	groups := []backend.GroupedView{
		{DisplayTitle: "Bravo", WatchedEpisodesSum: 10},
		{DisplayTitle: "Alpha", WatchedEpisodesSum: 10},
		{DisplayTitle: "Charlie", WatchedEpisodesSum: 12},
	}

	backend.SortGroupedViews(groups)

	want := []backend.GroupedView{
		{DisplayTitle: "Charlie", WatchedEpisodesSum: 12},
		{DisplayTitle: "Alpha", WatchedEpisodesSum: 10},
		{DisplayTitle: "Bravo", WatchedEpisodesSum: 10},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("sorted grouped views mismatch:\n got: %#v\nwant: %#v", groups, want)
	}
}

func TestSummarizeRetryErrors(t *testing.T) {
	got := backend.SummarizeRetryErrors([]string{"a", "b", "c", "d"})
	if got != "a; b; c; and 1 more" {
		t.Fatalf("SummarizeRetryErrors = %q, want %q", got, "a; b; c; and 1 more")
	}
}
