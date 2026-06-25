package domain

import (
	"errors"
	"strings"
	"time"
)

type SeasonName string

const (
	SeasonWinter SeasonName = "winter"
	SeasonSpring SeasonName = "spring"
	SeasonSummer SeasonName = "summer"
	SeasonFall   SeasonName = "fall"
)

const (
	minSeasonYear = 1900
	maxSeasonYear = 2100
)

// ErrInvalidSeason is returned when a year/name pair is not a valid MAL season.
var ErrInvalidSeason = errors.New("invalid season")

// Season is the MAL premiere season: a calendar year plus one of the four
// season names MAL assigns to anime.
type Season struct {
	Year int
	Name SeasonName
}

// SeasonalAnimeItem is a flat catalog entry surfaced on the seasonal browse
// view. It carries only the catalog-backed fields that are available without a
// user snapshot.
type SeasonalAnimeItem struct {
	ID             int
	Title          string
	MediaType      string
	StartDate      string
	ImageMediumURL string
	ImageLargeURL  string
	NumEpisodes    int
	// Genres are this anime's own MAL genres, used by the seasonal genre filter.
	// It stays nil until the entry's details (and thus genres) are hydrated.
	Genres []AnimeGenre
}

// NormalizeSeasonName lowercases and validates a MAL season label, reporting
// whether it is one of the four supported names.
func NormalizeSeasonName(value string) (SeasonName, bool) {
	switch SeasonName(strings.ToLower(strings.TrimSpace(value))) {
	case SeasonWinter:
		return SeasonWinter, true
	case SeasonSpring:
		return SeasonSpring, true
	case SeasonSummer:
		return SeasonSummer, true
	case SeasonFall:
		return SeasonFall, true
	default:
		return "", false
	}
}

// NewSeason validates a year/name pair into a Season.
func NewSeason(year int, name string) (Season, error) {
	seasonName, ok := NormalizeSeasonName(name)
	if !ok {
		return Season{}, ErrInvalidSeason
	}
	if year < minSeasonYear || year > maxSeasonYear {
		return Season{}, ErrInvalidSeason
	}
	return Season{Year: year, Name: seasonName}, nil
}

// CurrentSeason maps a moment in time to its MAL season using the standard
// month boundaries: Jan-Mar winter, Apr-Jun spring, Jul-Sep summer,
// Oct-Dec fall.
func CurrentSeason(now time.Time) Season {
	var name SeasonName
	switch now.Month() {
	case time.January, time.February, time.March:
		name = SeasonWinter
	case time.April, time.May, time.June:
		name = SeasonSpring
	case time.July, time.August, time.September:
		name = SeasonSummer
	default:
		name = SeasonFall
	}
	return Season{Year: now.Year(), Name: name}
}
