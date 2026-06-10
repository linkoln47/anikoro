package usecase

import (
	"context"
	"errors"
	"fmt"

	"test/internal/domain"
	"test/internal/ports"
)

var (
	ErrAnimeNotInCatalog    = errors.New("anime is not present in the local catalog; run sync first")
	ErrMALListUpdateFailed  = errors.New("failed to update MAL list entry")
	ErrListEntrySaveFailed  = errors.New("failed to save list entry")
	ErrInvalidListEditInput = errors.New("invalid list edit input")
)

// UpdatedUserAnimeListEntry is the canonical entry state after a successful
// MAL update and local upsert.
type UpdatedUserAnimeListEntry struct {
	AnimeID         int
	Title           string
	ListStatus      domain.AnimeListStatus
	Score           int
	WatchedEpisodes int
	NumEpisodes     int
}

type ListEditService struct {
	malWriter     ports.MALAnimeListWriter
	catalogRepo   ports.AnimeCatalogRepository
	userAnimeRepo ports.UserAnimeRepository
	logger        ports.SyncLogger
}

type ListEditServiceDependencies struct {
	MALWriter     ports.MALAnimeListWriter
	CatalogRepo   ports.AnimeCatalogRepository
	UserAnimeRepo ports.UserAnimeRepository
	Logger        ports.SyncLogger
}

func NewListEditService(deps ListEditServiceDependencies) *ListEditService {
	return &ListEditService{
		malWriter:     deps.MALWriter,
		catalogRepo:   deps.CatalogRepo,
		userAnimeRepo: deps.UserAnimeRepo,
		logger:        deps.Logger,
	}
}

func (service *ListEditService) logInfo(component, msg string, args ...any) {
	if service != nil && service.logger != nil {
		service.logger.Info(component, msg, args...)
	}
}

// UpdateUserAnimeListEntry validates the patch, pushes it to MAL with the
// user's token, and mirrors the canonical MAL state into the local snapshot.
func (service *ListEditService) UpdateUserAnimeListEntry(
	ctx context.Context,
	userID int64,
	token string,
	animeID int,
	patch domain.UserAnimeListPatch,
) (UpdatedUserAnimeListEntry, error) {
	ctx = ensureContext(ctx)

	if animeID <= 0 {
		return UpdatedUserAnimeListEntry{}, fmt.Errorf("%w: anime id must be positive", ErrInvalidListEditInput)
	}

	summary, found, err := service.catalogRepo.GetAnimeCatalogSummary(ctx, animeID)
	if err != nil {
		return UpdatedUserAnimeListEntry{}, fmt.Errorf("load anime catalog summary: %w", err)
	}
	if !found {
		return UpdatedUserAnimeListEntry{}, ErrAnimeNotInCatalog
	}

	if err := patch.Validate(summary.NumEpisodes); err != nil {
		return UpdatedUserAnimeListEntry{}, fmt.Errorf("%w: %w", ErrInvalidListEditInput, err)
	}

	state, err := service.malWriter.UpdateAnimeListStatus(ctx, token, animeID, patch)
	if err != nil {
		return UpdatedUserAnimeListEntry{}, fmt.Errorf("%w: %w", ErrMALListUpdateFailed, err)
	}

	listStatus, ok := domain.NormalizeAnimeListStatus(state.ListStatus)
	if !ok {
		return UpdatedUserAnimeListEntry{}, fmt.Errorf("%w: MAL returned unsupported list status %q", ErrMALListUpdateFailed, state.ListStatus)
	}

	title := summary.Title
	if title == "" {
		title = fmt.Sprintf("Anime #%d", animeID)
	}

	entry := domain.UserAnimeListEntry{
		ID:                 animeID,
		Title:              title,
		Score:              state.Score,
		NumEpisodesWatched: state.WatchedEpisodes,
		ListStatus:         listStatus,
	}
	if err := service.userAnimeRepo.UpsertUserAnimeItem(ctx, userID, entry); err != nil {
		return UpdatedUserAnimeListEntry{}, fmt.Errorf("%w: %w", ErrListEntrySaveFailed, err)
	}

	service.logInfo(
		"list_edit",
		"anime list entry updated",
		"user_id", userID,
		"anime_id", animeID,
		"status", string(listStatus),
		"score", state.Score,
		"watched", state.WatchedEpisodes,
	)

	return UpdatedUserAnimeListEntry{
		AnimeID:         animeID,
		Title:           title,
		ListStatus:      listStatus,
		Score:           state.Score,
		WatchedEpisodes: state.WatchedEpisodes,
		NumEpisodes:     summary.NumEpisodes,
	}, nil
}
