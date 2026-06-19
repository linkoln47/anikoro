package domain_test

import (
	"errors"
	"testing"
	"time"

	"test/internal/domain"
)

func TestNormalizeSeasonName(t *testing.T) {
	cases := []struct {
		input string
		want  domain.SeasonName
		ok    bool
	}{
		{"winter", domain.SeasonWinter, true},
		{"  Spring ", domain.SeasonSpring, true},
		{"SUMMER", domain.SeasonSummer, true},
		{"Fall", domain.SeasonFall, true},
		{"autumn", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		got, ok := domain.NormalizeSeasonName(tc.input)
		if ok != tc.ok || got != tc.want {
			t.Errorf("NormalizeSeasonName(%q) = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNewSeason(t *testing.T) {
	season, err := domain.NewSeason(2026, "Summer")
	if err != nil {
		t.Fatalf("NewSeason returned error: %v", err)
	}
	if season.Year != 2026 || season.Name != domain.SeasonSummer {
		t.Fatalf("NewSeason = %+v, want {2026 summer}", season)
	}

	for _, tc := range []struct {
		year int
		name string
	}{
		{2026, "autumn"},
		{1800, "winter"},
		{2200, "fall"},
	} {
		if _, err := domain.NewSeason(tc.year, tc.name); !errors.Is(err, domain.ErrInvalidSeason) {
			t.Errorf("NewSeason(%d, %q) error = %v, want ErrInvalidSeason", tc.year, tc.name, err)
		}
	}
}

func TestCurrentSeason(t *testing.T) {
	cases := []struct {
		when time.Time
		want domain.Season
	}{
		{time.Date(2026, time.January, 15, 0, 0, 0, 0, time.UTC), domain.Season{Year: 2026, Name: domain.SeasonWinter}},
		{time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC), domain.Season{Year: 2026, Name: domain.SeasonSpring}},
		{time.Date(2026, time.July, 31, 0, 0, 0, 0, time.UTC), domain.Season{Year: 2026, Name: domain.SeasonSummer}},
		{time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC), domain.Season{Year: 2026, Name: domain.SeasonFall}},
	}

	for _, tc := range cases {
		if got := domain.CurrentSeason(tc.when); got != tc.want {
			t.Errorf("CurrentSeason(%v) = %+v, want %+v", tc.when, got, tc.want)
		}
	}
}
