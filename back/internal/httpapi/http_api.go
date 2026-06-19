package httpapi

import (
	"context"
	"log/slog"

	"github.com/gorilla/mux"
	"test/internal/domain"
	"test/internal/ports"
	"test/internal/usecase"
)

type Config struct {
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	FrontendURL   string
	SessionSecret string
}

type AuthUsecase interface {
	GetValidToken(ctx context.Context, userID int64) (*domain.MALToken, error)
	CompleteMALLogin(ctx context.Context, code, verifier string) (domain.User, error)
	UpsertUserByPublicUsername(ctx context.Context, username string) (domain.User, error)
	ResolveUserByUsername(ctx context.Context, username string) (domain.User, error)
}

type AnimeQueryUsecase interface {
	ListAnime(ctx context.Context, userID int64) ([]domain.AnimeListItem, error)
	GetStats(ctx context.Context, userID int64) (domain.AnimeStats, error)
}

type SeasonQueryUsecase interface {
	CurrentSeason() domain.Season
	ListSeasonAnime(ctx context.Context, year int, name string) (domain.Season, []domain.SeasonalAnimeItem, error)
}

type SyncUsecase interface {
	RunSyncWithJob(ctx context.Context, userID int64, token string, reporter ports.SyncProgressReporter)
	RunPublicSyncWithJob(ctx context.Context, userID int64, username string, reporter ports.SyncProgressReporter)
}

type ListEditUsecase interface {
	UpdateUserAnimeListEntry(ctx context.Context, userID int64, token string, animeID int, patch domain.UserAnimeListPatch) (usecase.UpdatedUserAnimeListEntry, error)
	RemoveUserAnimeListEntry(ctx context.Context, userID int64, token string, animeID int) error
}

type Dependencies struct {
	Config        Config
	Auth          AuthUsecase
	AnimeQueries  AnimeQueryUsecase
	SeasonQueries SeasonQueryUsecase
	Sync          SyncUsecase
	ListEdits     ListEditUsecase
	SyncJobs      SyncJobStore
	Logger        *slog.Logger
}

type HTTPAPI struct {
	config        Config
	auth          AuthUsecase
	animeQueries  AnimeQueryUsecase
	seasonQueries SeasonQueryUsecase
	sync          SyncUsecase
	listEdits     ListEditUsecase
	syncJobs      SyncJobStore
	logger        *slog.Logger
}

func New(deps Dependencies) *HTTPAPI {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &HTTPAPI{
		config:        deps.Config,
		auth:          deps.Auth,
		animeQueries:  deps.AnimeQueries,
		seasonQueries: deps.SeasonQueries,
		sync:          deps.Sync,
		listEdits:     deps.ListEdits,
		syncJobs:      deps.SyncJobs,
		logger:        logger,
	}
}

func (api *HTTPAPI) SetupRouter() *mux.Router {
	r := mux.NewRouter()

	routes := r.PathPrefix("/api").Subrouter()
	routes.HandleFunc("/auth/mal/start", api.startMALAuthHandler()).Methods("GET")
	routes.HandleFunc("/auth/mal/callback", api.completeMALAuthHandler()).Methods("GET")
	routes.HandleFunc("/auth/logout", api.logoutHandler()).Methods("POST")
	routes.HandleFunc("/me", api.meHandler()).Methods("GET")
	routes.HandleFunc("/anime", api.getAnimeHandler()).Methods("GET")
	routes.HandleFunc("/anime/{anime_id}/list-status", api.updateListEntryHandler()).Methods("PATCH")
	routes.HandleFunc("/anime/{anime_id}/list-status", api.removeListEntryHandler()).Methods("DELETE")
	routes.HandleFunc("/sync", api.syncHandler()).Methods("POST")
	routes.HandleFunc("/sync/jobs/{job_id}", api.getSyncJobHandler()).Methods("GET")
	routes.HandleFunc("/sync/jobs/{job_id}/events", api.syncJobEventsHandler()).Methods("GET")
	routes.HandleFunc("/stats", api.getStatsHandler()).Methods("GET")
	routes.HandleFunc("/season", api.getCurrentSeasonHandler()).Methods("GET")
	routes.HandleFunc("/season/{year}/{season}", api.getSeasonHandler()).Methods("GET")
	routes.HandleFunc("/public/sync", api.publicSyncHandler()).Methods("POST")
	routes.HandleFunc("/public/anime/{username}", api.getPublicAnimeHandler()).Methods("GET")
	routes.HandleFunc("/public/stats/{username}", api.getPublicStatsHandler()).Methods("GET")

	return r
}

func (api *HTTPAPI) logInfo(component, msg string, args ...any) {
	api.logger.Info(msg, withComponent(component, args)...)
}

func (api *HTTPAPI) logWarn(component, msg string, args ...any) {
	api.logger.Warn(msg, withComponent(component, args)...)
}

func (api *HTTPAPI) logError(component, msg string, args ...any) {
	api.logger.Error(msg, withComponent(component, args)...)
}

func withComponent(component string, args []any) []any {
	fields := make([]any, 0, len(args)+2)
	fields = append(fields, "component", component)
	fields = append(fields, args...)
	return fields
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
