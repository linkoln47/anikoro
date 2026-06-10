package domain

import (
	"errors"
	"fmt"
)

const (
	AnimeListScoreMin = 0
	AnimeListScoreMax = 10
)

var (
	ErrEmptyAnimeListPatch        = errors.New("at least one of status, score, or num_watched_episodes is required")
	ErrInvalidAnimeListStatus     = errors.New("unsupported anime list status")
	ErrInvalidAnimeListScore      = fmt.Errorf("score must be between %d and %d", AnimeListScoreMin, AnimeListScoreMax)
	ErrNegativeWatchedEpisodes    = errors.New("num_watched_episodes must be >= 0")
	ErrWatchedEpisodesExceedTotal = errors.New("num_watched_episodes exceeds the total episode count")
)

// UserAnimeListPatch is a partial update of one anime entry in the user's
// list. Nil fields are left unchanged.
type UserAnimeListPatch struct {
	Status          *AnimeListStatus
	Score           *int
	WatchedEpisodes *int
}

func (patch UserAnimeListPatch) IsEmpty() bool {
	return patch.Status == nil && patch.Score == nil && patch.WatchedEpisodes == nil
}

// Validate checks patch fields against domain rules. totalEpisodes caps the
// watched episode count; pass 0 when the total is unknown to skip the cap.
func (patch UserAnimeListPatch) Validate(totalEpisodes int) error {
	if patch.IsEmpty() {
		return ErrEmptyAnimeListPatch
	}

	if patch.Status != nil {
		if _, ok := NormalizeAnimeListStatus(string(*patch.Status)); !ok {
			return fmt.Errorf("%w: %q", ErrInvalidAnimeListStatus, string(*patch.Status))
		}
	}

	if patch.Score != nil {
		if *patch.Score < AnimeListScoreMin || *patch.Score > AnimeListScoreMax {
			return ErrInvalidAnimeListScore
		}
	}

	if patch.WatchedEpisodes != nil {
		if *patch.WatchedEpisodes < 0 {
			return ErrNegativeWatchedEpisodes
		}
		if totalEpisodes > 0 && *patch.WatchedEpisodes > totalEpisodes {
			return fmt.Errorf("%w: %d > %d", ErrWatchedEpisodesExceedTotal, *patch.WatchedEpisodes, totalEpisodes)
		}
	}

	return nil
}
