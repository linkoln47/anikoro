package usecase

import (
	"context"
	"strings"
	"time"
)

const DetailsCacheTTL = 168 * time.Hour

type MALAuth struct {
	BearerToken string
	ClientID    string
}

func BearerMALAuth(token string) MALAuth {
	return MALAuth{BearerToken: strings.TrimSpace(token)}
}

func ClientIDMALAuth(clientID string) MALAuth {
	return MALAuth{ClientID: strings.TrimSpace(clientID)}
}

type CachedAnimeDetails struct {
	Details   AnimeDetails
	UpdatedAt time.Time
	Resolved  bool
}

func (details CachedAnimeDetails) IsUsable() bool {
	return details.Resolved || details.Details.MediaType != ""
}

func (details CachedAnimeDetails) IsFresh(now time.Time) bool {
	return details.IsUsable() && !details.UpdatedAt.IsZero() && now.Sub(details.UpdatedAt) <= DetailsCacheTTL
}

type MALAnimeClient interface {
	FetchCompletedList(ctx context.Context, token string) ([]CompletedAnimeEntry, error)
	FetchPublicCompletedList(ctx context.Context, username string) ([]CompletedAnimeEntry, error)
	FetchAnimeDetails(ctx context.Context, auth MALAuth, animeID int, cache AnimeDetailsCacheStore, mode AnimeDetailsFetchMode) (AnimeDetails, error)
}

type AnimeDetailsFetchMode string

const (
	AnimeDetailsFetchPrimary AnimeDetailsFetchMode = "primary"
	AnimeDetailsFetchRetry   AnimeDetailsFetchMode = "retry"
)

type DetailsCache interface {
	OpenDetailsCache(ctx context.Context) (AnimeDetailsCacheStore, error)
}

type AnimeDetailsCacheStore interface {
	Lookup(animeID int) (CachedAnimeDetails, bool)
	StoreResolved(animeID int, details AnimeDetails) error
	FlushPending() error
}

type SyncAnimeRepository interface {
	AnimeCatalogRepository
	UserAnimeRepository
	FranchiseRepository
}

type UserAnimeRepository interface {
	ClearUserAnimeSnapshot(ctx context.Context, userID int64) error
	ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []CompletedAnimeEntry) error
}

type AnimeCatalogRepository interface {
	UpsertAnimeCatalogStubs(ctx context.Context, animeIDs []int) error
	GetAnimeCatalogState(ctx context.Context, animeID int) (AnimeCatalogState, bool, error)
	GetAnimeCatalogStates(ctx context.Context, animeIDs []int) (map[int]AnimeCatalogState, error)
	GetAnimeCatalogMediaType(ctx context.Context, animeID int) (string, error)
	ListAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error)
	ListAnimeRelationIDsBySourceIDs(ctx context.Context, animeIDs []int) (map[int][]int, error)
	ListUndirectedAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error)
	SaveAnimeCatalogDetailsBatch(ctx context.Context, detailsBatch []AnimeDetails) error
}

type FranchiseRepository interface {
	RefreshAnimeFranchises(ctx context.Context, seedIDs []int) error
}

type AnimeCatalogHydrator interface {
	HydrateCatalogGraph(ctx context.Context, auth MALAuth, seedIDs []int, cache AnimeDetailsCacheStore, reporter SyncProgressReporter) error
}

type SyncProgressReporter interface {
	Start(message string)
	Update(phase string, current, total int, message string)
	UpdateThrottled(phase string, current, total int, message string, interval time.Duration)
	Complete(message string)
	Fail(err error)
}

type noopSyncProgressReporter struct{}

func (noopSyncProgressReporter) Start(string)                                            {}
func (noopSyncProgressReporter) Update(string, int, int, string)                         {}
func (noopSyncProgressReporter) UpdateThrottled(string, int, int, string, time.Duration) {}
func (noopSyncProgressReporter) Complete(string)                                         {}
func (noopSyncProgressReporter) Fail(error)                                              {}

func ensureSyncProgressReporter(reporter SyncProgressReporter) SyncProgressReporter {
	if reporter == nil {
		return noopSyncProgressReporter{}
	}
	return reporter
}

type UserSyncGuard interface {
	TryBeginUserSync(userID int64) bool
	FinishUserSync(userID int64)
}

type SyncLogger interface {
	Debug(component, msg string, args ...any)
	Info(component, msg string, args ...any)
	Warn(component, msg string, args ...any)
	Error(component, msg string, args ...any)
}

type MALClientIDProvider interface {
	MALClientID() string
}
