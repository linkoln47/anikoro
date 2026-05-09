package ports

import (
	"context"
	"time"

	"test/internal/domain"
)

const DetailsCacheTTL = 168 * time.Hour

type SyncProgressPhase string

const (
	SyncJobPhaseQueued           SyncProgressPhase = "queued"
	SyncJobPhaseFetchingList     SyncProgressPhase = "fetching_list"
	SyncJobPhaseListFetched      SyncProgressPhase = "list_fetched"
	SyncJobPhaseSavingSnapshot   SyncProgressPhase = "saving_snapshot"
	SyncJobPhaseHydratingCatalog SyncProgressPhase = "hydrating_catalog"
	SyncJobPhaseGrouping         SyncProgressPhase = "grouping"
	SyncJobPhaseDone             SyncProgressPhase = "done"
)

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
	FetchAnimeList(ctx context.Context, token string) ([]domain.UserAnimeListEntry, error)
	FetchPublicAnimeList(ctx context.Context, username string) ([]domain.UserAnimeListEntry, error)
	FetchAnimeDetails(ctx context.Context, token string, animeID int, cache AnimeDetailsCacheStore, mode AnimeDetailsFetchMode) (domain.AnimeDetails, error)
	FetchPublicAnimeDetails(ctx context.Context, animeID int, cache AnimeDetailsCacheStore, mode AnimeDetailsFetchMode) (domain.AnimeDetails, error)
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

type MALOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

type MALOAuthClient interface {
	ExchangeCodeForToken(ctx context.Context, config MALOAuthConfig, code, verifier string) (*domain.MALToken, error)
	FetchCurrentUser(ctx context.Context, token string) (domain.MALUserProfile, error)
}

type AuthRepository interface {
	UpsertMALUser(ctx context.Context, profile domain.MALUserProfile) (domain.User, error)
	UpsertUserByPublicUsername(ctx context.Context, username string) (domain.User, error)
	UserByUsername(ctx context.Context, username string) (domain.User, bool, error)
	LoadToken(ctx context.Context, userID int64) (domain.MALToken, bool, error)
	SaveToken(ctx context.Context, userID int64, token domain.MALToken) error
}

type SyncAnimeRepository interface {
	AnimeCatalogRepository
	UserAnimeRepository
	FranchiseRepository
}

type UserAnimeRepository interface {
	ClearUserAnimeSnapshot(ctx context.Context, userID int64) error
	ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []domain.UserAnimeListEntry) error
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
	HydrateCatalogGraph(ctx context.Context, token string, seedIDs []int, cache AnimeDetailsCacheStore, reporter SyncProgressReporter) error
	HydratePublicCatalogGraph(ctx context.Context, seedIDs []int, cache AnimeDetailsCacheStore, reporter SyncProgressReporter) error
}

type SyncProgressReporter interface {
	Start(message string)
	Update(phase SyncProgressPhase, current, total int, message string)
	UpdateThrottled(phase SyncProgressPhase, current, total int, message string, interval time.Duration)
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
