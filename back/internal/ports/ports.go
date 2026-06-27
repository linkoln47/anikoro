package ports

import (
	"context"
	"errors"
	"time"

	"test/internal/domain"
)

const DetailsCacheTTL = 24 * time.Hour

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
	FetchAnimeDetails(ctx context.Context, token string, animeID int, mode AnimeDetailsFetchMode) (domain.AnimeDetails, error)
	FetchPublicAnimeDetails(ctx context.Context, animeID int, mode AnimeDetailsFetchMode) (domain.AnimeDetails, error)
}

// MALAnimeListWriter pushes list entry changes to the MAL account that owns
// the token and returns the canonical state MAL reports back.
type MALAnimeListWriter interface {
	UpdateAnimeListStatus(ctx context.Context, token string, animeID int, patch domain.UserAnimeListPatch) (domain.AnimeUserListState, error)
	DeleteAnimeListStatus(ctx context.Context, token string, animeID int) error
}

type AnimeDetailsFetchMode string

const (
	AnimeDetailsFetchPrimary AnimeDetailsFetchMode = "primary"
	AnimeDetailsFetchRetry   AnimeDetailsFetchMode = "retry"
)

type AnimeDetailsFetchErrorKind string

const (
	AnimeDetailsFetchErrorNotFound  AnimeDetailsFetchErrorKind = "not_found"
	AnimeDetailsFetchErrorTransient AnimeDetailsFetchErrorKind = "transient"
	AnimeDetailsFetchErrorFatal     AnimeDetailsFetchErrorKind = "fatal"
)

// AnimeDetailsFetchError classifies failures at the MAL boundary so use cases
// can distinguish a missing catalog entry from a retryable outage.
type AnimeDetailsFetchError struct {
	AnimeID    int
	StatusCode int
	Kind       AnimeDetailsFetchErrorKind
	Retryable  bool
	Err        error
}

func (err *AnimeDetailsFetchError) Error() string {
	if err == nil {
		return ""
	}
	if err.Err != nil {
		return err.Err.Error()
	}
	return string(err.Kind)
}

func (err *AnimeDetailsFetchError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

func IsAnimeDetailsNotFound(err error) bool {
	var fetchErr *AnimeDetailsFetchError
	return errors.As(err, &fetchErr) && fetchErr.Kind == AnimeDetailsFetchErrorNotFound
}

// IsAnimeDetailsRetryable preserves the existing retry behavior for clients
// that have not adopted AnimeDetailsFetchError yet.
func IsAnimeDetailsRetryable(err error) bool {
	var fetchErr *AnimeDetailsFetchError
	if !errors.As(err, &fetchErr) {
		return true
	}
	return fetchErr.Retryable
}

type DetailsCache interface {
	OpenDetailsCache(ctx context.Context) (AnimeDetailsCacheStore, error)
}

// AnimeDetailsCacheStore is a write-ahead staging buffer for anime details
// fetched from MAL: entries are staged before the database write and removed
// once persisted, so the database stays the single source of truth.
type AnimeDetailsCacheStore interface {
	StoreResolved(animeID int, details domain.AnimeDetails) error
	StagedDetails() []CachedAnimeDetails
	MarkPersisted(animeIDs []int) error
	FlushPending() error
}

// AnimeHydrationFailureStore temporarily quarantines MAL ids that cannot be
// resolved. It is an operational cache, not catalog state: PostgreSQL remains
// the source of truth and unresolved rows keep resolved=false.
type AnimeHydrationFailureStore interface {
	ShouldAttempt(animeID int, now time.Time) bool
	DeferredCount(now time.Time) int
	RecordNotFound(animeID int, attemptedAt time.Time) (time.Time, error)
	// RecordTransientFailure backs off an id that returned a transient error
	// (timeout, 5xx) after all retries were exhausted. The backoff is shorter
	// than for 404 since the endpoint may recover.
	RecordTransientFailure(animeID int, attemptedAt time.Time) (time.Time, error)
	MarkSucceeded(animeIDs []int) error
}

type AnimeReadRepository interface {
	ListAnime(ctx context.Context, userID int64) ([]domain.AnimeListItem, error)
	GetStats(ctx context.Context, userID int64) (domain.AnimeStats, error)
	// GetFranchise resolves the global franchise grouping for a single anime id
	// from the shared catalog. When userID is positive the caller's list marks
	// are decorated onto the entries; userID 0 yields the same grouping with the
	// user-only fields zeroed. The boolean reports catalog presence.
	GetFranchise(ctx context.Context, animeID int, userID int64) (domain.AnimeListItem, bool, error)
	// ListFranchises returns a page of franchise groups from the catalog, each
	// reduced to its representative title, with standalone catalog anime surfaced
	// as single-member groups. The query filters by representative media type and
	// title and windows the result with Limit/Offset; the returned int is the
	// total number of groups matching the filters (ignoring the window) for the
	// caller's paging UI. It reads only the global catalog tables and is not
	// scoped to a user.
	ListFranchises(ctx context.Context, query domain.FranchiseQuery) ([]domain.FranchiseSummary, int, error)
	// ListGenres returns the catalog's genre universe (genres attached to at least
	// one anime), sorted by name. It backs the franchise grid's genre filter, which
	// cannot derive its options from a single loaded page. Not scoped to a user.
	ListGenres(ctx context.Context) ([]domain.AnimeGenre, error)
}

// SeasonReadRepository lists catalog anime that premiered in a given MAL
// season. It reads only the global anime_catalog table and is not scoped to a
// user.
type SeasonReadRepository interface {
	ListSeasonAnime(ctx context.Context, season domain.Season) ([]domain.SeasonalAnimeItem, error)
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
	UserByUsername(ctx context.Context, username string) (domain.User, bool, error)
	// CreateUserWithPassword creates a native account. It must surface
	// ErrEmailTaken / ErrUsernameTaken on unique conflicts.
	CreateUserWithPassword(ctx context.Context, email, username, passwordHash string) (domain.User, error)
	// UserCredentialsByEmail loads a native account and its stored password hash
	// for login. The boolean reports whether an account with that email exists.
	UserCredentialsByEmail(ctx context.Context, email string) (domain.User, string, bool, error)
	// AttachMALProfile creates the MAL identity for an existing native user. It
	// must surface ErrMALAlreadyLinked when the MAL account belongs to a
	// different user, and ErrMALProfileExists when this user already linked one.
	AttachMALProfile(ctx context.Context, userID int64, profile domain.MALUserProfile) (domain.MALProfile, domain.User, error)
	// UnlinkMALProfile deletes the user's MAL profile (and, by cascade, its
	// token), but leaves the synced anime snapshot (user_anime_items) untouched.
	UnlinkMALProfile(ctx context.Context, userID int64) (domain.User, error)
	// LoadToken loads the MAL token for the profile owned by userID.
	LoadToken(ctx context.Context, userID int64) (domain.MALToken, bool, error)
	// SaveToken upserts the MAL token for a given mal_profile_id.
	SaveToken(ctx context.Context, malProfileID int64, token domain.MALToken) error
}

// ErrPasswordMismatch is returned by PasswordHasher.Compare when the password
// does not match the stored hash. Use cases collapse it into a generic
// invalid-credentials response so callers cannot distinguish it from an
// unknown account.
var ErrPasswordMismatch = errors.New("password does not match")

// PasswordHasher hashes and verifies native account passwords. The hash format
// is opaque to callers so the implementation (bcrypt today) can change without
// touching use cases.
type PasswordHasher interface {
	Hash(plainPassword string) (string, error)
	Compare(hashedPassword, plainPassword string) error
}

type SyncAnimeRepository interface {
	AnimeCatalogRepository
	UserAnimeRepository
	FranchiseRepository
}

type UserAnimeRepository interface {
	ClearUserAnimeSnapshot(ctx context.Context, userID int64) error
	ReplaceUserAnimeItems(ctx context.Context, userID int64, entries []domain.UserAnimeListEntry) error
	UpsertUserAnimeItem(ctx context.Context, userID int64, entry domain.UserAnimeListEntry) error
	DeleteUserAnimeItem(ctx context.Context, userID int64, animeID int) error
}

type AnimeCatalogRepository interface {
	UpsertAnimeCatalogStubs(ctx context.Context, animeIDs []int) error
	GetAnimeCatalogState(ctx context.Context, animeID int) (domain.AnimeCatalogState, bool, error)
	GetAnimeCatalogStates(ctx context.Context, animeIDs []int) (map[int]domain.AnimeCatalogState, error)
	GetAnimeCatalogMediaType(ctx context.Context, animeID int) (string, error)
	GetAnimeCatalogSummary(ctx context.Context, animeID int) (domain.AnimeCatalogSummary, bool, error)
	ListAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error)
	ListAnimeRelationIDsBySourceIDs(ctx context.Context, animeIDs []int) (map[int][]int, error)
	ListUndirectedAnimeRelationIDs(ctx context.Context, animeID int) ([]int, error)
	SaveAnimeCatalogDetailsBatch(ctx context.Context, detailsBatch []domain.AnimeDetails) error
	// ListUngroupedResolvedCatalogIDs returns ids of resolved catalog entries
	// that have no row in anime_franchises, smallest id first, capped at limit.
	// The reconciliation pass uses it to rebuild franchise groupings that were
	// skipped when a previous RefreshAnimeFranchises call failed after details
	// were already persisted. A non-positive limit returns no ids.
	ListUngroupedResolvedCatalogIDs(ctx context.Context, limit int) ([]int, error)
}

type FranchiseRepository interface {
	RefreshAnimeFranchises(ctx context.Context, seedIDs []int) error
}

// AnimeCatalogHydrationSink is the write-side port for atomic catalog
// persistence. SaveAnimeCatalogDetailsWithFranchises saves anime details,
// relations, and the resulting franchise groupings in one transaction so that
// a resolved=true entry always has consistent anime_franchises rows.
type AnimeCatalogHydrationSink interface {
	SaveAnimeCatalogDetailsWithFranchises(ctx context.Context, detailsBatch []domain.AnimeDetails) error
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
