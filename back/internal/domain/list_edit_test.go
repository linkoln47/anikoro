package domain

import (
	"errors"
	"testing"
)

func intPtr(value int) *int { return &value }

func statusPtr(status AnimeListStatus) *AnimeListStatus { return &status }

func TestUserAnimeListPatchValidate(t *testing.T) {
	tests := []struct {
		name          string
		patch         UserAnimeListPatch
		totalEpisodes int
		wantErr       error
	}{
		{
			name:    "empty patch is rejected",
			patch:   UserAnimeListPatch{},
			wantErr: ErrEmptyAnimeListPatch,
		},
		{
			name:  "valid status",
			patch: UserAnimeListPatch{Status: statusPtr(AnimeListStatusWatching)},
		},
		{
			name:    "unknown status is rejected",
			patch:   UserAnimeListPatch{Status: statusPtr(AnimeListStatus("rewatching"))},
			wantErr: ErrInvalidAnimeListStatus,
		},
		{
			name:  "score lower bound",
			patch: UserAnimeListPatch{Score: intPtr(0)},
		},
		{
			name:  "score upper bound",
			patch: UserAnimeListPatch{Score: intPtr(10)},
		},
		{
			name:    "score above range is rejected",
			patch:   UserAnimeListPatch{Score: intPtr(11)},
			wantErr: ErrInvalidAnimeListScore,
		},
		{
			name:    "negative score is rejected",
			patch:   UserAnimeListPatch{Score: intPtr(-1)},
			wantErr: ErrInvalidAnimeListScore,
		},
		{
			name:    "negative watched episodes are rejected",
			patch:   UserAnimeListPatch{WatchedEpisodes: intPtr(-1)},
			wantErr: ErrNegativeWatchedEpisodes,
		},
		{
			name:          "watched episodes within total",
			patch:         UserAnimeListPatch{WatchedEpisodes: intPtr(12)},
			totalEpisodes: 12,
		},
		{
			name:          "watched episodes above total are rejected",
			patch:         UserAnimeListPatch{WatchedEpisodes: intPtr(13)},
			totalEpisodes: 12,
			wantErr:       ErrWatchedEpisodesExceedTotal,
		},
		{
			name:          "unknown total skips the cap",
			patch:         UserAnimeListPatch{WatchedEpisodes: intPtr(9999)},
			totalEpisodes: 0,
		},
		{
			name: "combined valid patch",
			patch: UserAnimeListPatch{
				Status:          statusPtr(AnimeListStatusCompleted),
				Score:           intPtr(8),
				WatchedEpisodes: intPtr(24),
			},
			totalEpisodes: 24,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.patch.Validate(test.totalEpisodes)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() returned unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}
