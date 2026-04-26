package ports

import (
	"context"
	"time"

	"test/internal/domain"
)

const DetailsCacheTTL = 168 * time.Hour

const (
	SyncJobPhaseQueued           = "queued"
	SyncJobPhaseFetchingList     = "fetching_list"
	SyncJobPhaseListFetched      = "list_fetched"
	SyncJobPhaseSavingSnapshot   = "saving_snapshot"
	SyncJobPhaseHydratingCatalog = "hydrating_catalog"
	SyncJobPhaseGrouping         = "grouping"
	SyncJobPhaseDone             = "done"
)

type MALAuth struct {
	BearerToken string
	ClientID    string
}

type CachedAnimeDetails struct {
	Details   domain.AnimeDetails
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
	FetchCompletedList(ctx context.Context, token string) ([]domain.CompletedAnimeEntry, error)
	FetchPublicCompletedList(ctx context.Context, username string) ([]domain.CompletedAnimeEntry, error)
	FetchAnimeDetails(ctx context.Context, auth MALAuth, animeID int, cache AnimeDetailsCacheStore, mode AnimeDetailsFetchMode) (domain.AnimeDetails, error)
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
	StoreResolved(animeID int, details domain.AnimeDetails) error
	FlushPending() error
}

type AnimeReadRepository interface {
	ListAnime(ctx context.Context, userID int64) ([]domain.AnimeListItem, error)
	GetStats(ctx context.Context, userID int64) (domain.AnimeStats, error)
}

type SyncAnimeRepository interface {
	AnimeCatalogRepository
	UserAnimeRepository
	FranchiseRepository
}

type UserAnimeRepository interface {
	ClearUserAnimeSnapshot(ctx context.Context, userID int64) error
	ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []domain.CompletedAnimeEntry) error
}

type AnimeCatalogRepository interface {
	UpsertAnimeCatalogStubs(ctx context.Context, animeIDs []int) error
	GetAnimeCatalogState(ctx context.Context, animeID int) (domain.AnimeCatalogState, bool, error)
	GetAnimeCatalogStates(ctx context.Context, animeIDs []int) (map[int]domain.AnimeCatalogState, error)
	GetAnimeCatalogMediaType(ctx context.Context, animeID int) (string, error)
	ListAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error)
	ListAnimeRelationIDsBySourceIDs(ctx context.Context, animeIDs []int) (map[int][]int, error)
	ListUndirectedAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error)
	SaveAnimeCatalogDetailsBatch(ctx context.Context, detailsBatch []domain.AnimeDetails) error
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
