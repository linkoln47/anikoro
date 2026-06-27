// Pure season rules shared by the seasonal browse view. No browser APIs,
// React state, or network calls live here.

export const SEASON_NAMES = ['winter', 'spring', 'summer', 'fall']

export const SEASON_LABELS = {
  winter: 'Winter',
  spring: 'Spring',
  summer: 'Summer',
  fall: 'Fall',
}

export const MIN_SEASON_YEAR = 1900
export const MAX_SEASON_YEAR = 2100

const titleCollator = new Intl.Collator('en', { sensitivity: 'base', numeric: true })

export function isValidSeasonName(value) {
  return SEASON_NAMES.includes(value)
}

export function isValidSeasonYear(value) {
  return Number.isInteger(value) && value >= MIN_SEASON_YEAR && value <= MAX_SEASON_YEAR
}

// getCurrentSeason mirrors the backend month boundaries so client navigation
// and the server agree on "the current season".
export function getCurrentSeason(date = new Date()) {
  const month = date.getMonth() // 0-based
  let season

  if (month <= 2) {
    season = 'winter'
  } else if (month <= 4) {
    season = 'spring'
  } else if (month <= 8) {
    season = 'summer'
  } else {
    season = 'fall'
  }

  return { year: date.getFullYear(), season }
}

// getAdjacentSeason moves by `delta` seasons (e.g. -1 previous, +1 next),
// wrapping the year at season boundaries.
export function getAdjacentSeason({ year, season }, delta) {
  const index = SEASON_NAMES.indexOf(season)
  if (index < 0) {
    return { year, season }
  }

  const absolute = year * SEASON_NAMES.length + index + delta
  const nextYear = Math.floor(absolute / SEASON_NAMES.length)
  const nextIndex = ((absolute % SEASON_NAMES.length) + SEASON_NAMES.length) % SEASON_NAMES.length

  return { year: nextYear, season: SEASON_NAMES[nextIndex] }
}

// collectSeasonGenres returns the unique genres present across the given anime,
// deduplicated by id and sorted by name. It backs the seasonal genre filter's
// available-options list.
export function collectSeasonGenres(anime) {
  const items = Array.isArray(anime) ? anime : []
  const byId = new Map()

  for (const item of items) {
    for (const genre of item?.genres ?? []) {
      if (genre && Number.isInteger(genre.id) && !byId.has(genre.id)) {
        byId.set(genre.id, { id: genre.id, name: genre.name ?? '' })
      }
    }
  }

  return [...byId.values()].sort((left, right) =>
    titleCollator.compare(left.name, right.name),
  )
}

// filterSeasonAnimeByGenres keeps only the anime that carry every selected genre
// id (AND semantics, so adding genres narrows the list). An empty selection
// returns the input unchanged.
export function filterSeasonAnimeByGenres(anime, selectedGenreIds) {
  const items = Array.isArray(anime) ? anime : []
  const selected = Array.isArray(selectedGenreIds) ? selectedGenreIds : []
  if (selected.length === 0) {
    return items
  }

  return items.filter((item) => {
    const ids = new Set((item?.genres ?? []).map((genre) => genre.id))
    return selected.every((id) => ids.has(id))
  })
}

// Genres hidden by the R18+ gate when it is disabled. Matched case-insensitively
// by name (MAL's canonical labels; the corresponding ids 9/12/49 are stable too,
// but names keep this list self-documenting).
export const EXPLICIT_GENRE_NAMES = ['ecchi', 'hentai', 'erotica']

const explicitGenreNameSet = new Set(EXPLICIT_GENRE_NAMES)

// hasExplicitGenre reports whether an anime carries any genre the R18+ gate hides.
export function hasExplicitGenre(item) {
  return (item?.genres ?? []).some((genre) =>
    explicitGenreNameSet.has((genre?.name ?? '').trim().toLowerCase()),
  )
}

// filterOutExplicitAnime drops anime carrying an explicit genre, backing the R18+
// gate's "off" state. Non-array input returns an empty list, mirroring the other
// defensive helpers here.
export function filterOutExplicitAnime(anime) {
  const items = Array.isArray(anime) ? anime : []
  return items.filter((item) => !hasExplicitGenre(item))
}

// filterOutExplicitGenres drops the explicit genres from a genre option list. It
// keeps the franchise/season filter dropdown free of Ecchi/Hentai/Erotica while
// the R18+ gate is off, matching the gated grid.
export function filterOutExplicitGenres(genres) {
  const items = Array.isArray(genres) ? genres : []
  return items.filter(
    (genre) => !explicitGenreNameSet.has((genre?.name ?? '').trim().toLowerCase()),
  )
}

// filterSeasonAnimeByMediaType keeps only anime of the given media type (e.g. "tv",
// "movie"); an empty type returns the input unchanged. Backs the seasonal
// media-type filter, mirroring the franchise grid's server-side media filter.
export function filterSeasonAnimeByMediaType(anime, mediaType) {
  const items = Array.isArray(anime) ? anime : []
  const wanted = (mediaType ?? '').trim().toLowerCase()
  if (wanted === '') {
    return items
  }

  return items.filter((item) => (item?.media_type ?? '').trim().toLowerCase() === wanted)
}

// MAL's main "Genres" bucket — the explicit user-facing list. Anything not matched
// by one of the named buckets below falls into "Themes" (MAL's catch-all).
export const MAIN_GENRE_NAMES = [
  'action', 'adventure', 'avant garde', 'award winning', 'boys love', 'comedy',
  'drama', 'fantasy', 'girls love', 'gourmet', 'horror', 'mystery', 'romance',
  'sci-fi', 'slice of life', 'sports', 'supernatural', 'suspense',
]

// MAL demographic genres (target audience).
export const DEMOGRAPHIC_GENRE_NAMES = ['josei', 'kids', 'seinen', 'shoujo', 'shounen']

// Sections the filter popover renders, in display order. `names: null` marks the
// catch-all bucket; every genre not in a named bucket lands there.
const GENRE_SECTIONS = [
  { key: 'genres', label: 'Genres', names: new Set(MAIN_GENRE_NAMES) },
  { key: 'explicit', label: 'Explicit Genres', names: new Set(EXPLICIT_GENRE_NAMES) },
  { key: 'themes', label: 'Themes', names: null },
  { key: 'demographics', label: 'Demographics', names: new Set(DEMOGRAPHIC_GENRE_NAMES) },
]

// groupSeasonGenres buckets the season's available genres into the filter sections
// (Genres, Explicit Genres, Themes, Demographics) in display order, matched
// case-insensitively by name. Unknown genres land in Themes; empty sections are
// dropped. Input order is preserved within each bucket.
export function groupSeasonGenres(genres) {
  const items = Array.isArray(genres) ? genres : []
  const buckets = new Map(GENRE_SECTIONS.map((section) => [section.key, []]))

  for (const genre of items) {
    const name = (genre?.name ?? '').trim().toLowerCase()
    const match = GENRE_SECTIONS.find((section) => section.names?.has(name))
    buckets.get(match ? match.key : 'themes').push(genre)
  }

  return GENRE_SECTIONS.filter((section) => buckets.get(section.key).length > 0).map(
    (section) => ({ key: section.key, label: section.label, genres: buckets.get(section.key) }),
  )
}

export const SEASON_SORT_KEYS = ['title', 'date', 'episodes', 'score']

function readDateValue(value) {
  if (!value) {
    return 0
  }

  const time = new Date(value).getTime()
  return Number.isNaN(time) ? 0 : time
}

// sortSeasonAnime returns a sorted copy of catalog-backed fields: title, air date,
// episode count, and MAL score (mean_score — descending, unscored titles last).
export function sortSeasonAnime(anime, sortKey) {
  const items = Array.isArray(anime) ? [...anime] : []

  switch (sortKey) {
    case 'date':
      return items.sort((left, right) => {
        const dateCompare = readDateValue(right.start_date) - readDateValue(left.start_date)
        if (dateCompare !== 0) {
          return dateCompare
        }
        return titleCollator.compare(left.title ?? '', right.title ?? '')
      })
    case 'episodes':
      return items.sort((left, right) => {
        const episodeCompare = (right.num_episodes ?? 0) - (left.num_episodes ?? 0)
        if (episodeCompare !== 0) {
          return episodeCompare
        }
        return titleCollator.compare(left.title ?? '', right.title ?? '')
      })
    case 'score':
      return items.sort((left, right) => {
        const scoreCompare = (right.mean_score ?? 0) - (left.mean_score ?? 0)
        if (scoreCompare !== 0) {
          return scoreCompare
        }
        return titleCollator.compare(left.title ?? '', right.title ?? '')
      })
    case 'title':
    default:
      return items.sort((left, right) =>
        titleCollator.compare(left.title ?? '', right.title ?? ''),
      )
  }
}
