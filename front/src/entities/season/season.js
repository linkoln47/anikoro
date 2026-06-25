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
  } else if (month <= 5) {
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

export const SEASON_SORT_KEYS = ['title', 'date', 'episodes']

function readDateValue(value) {
  if (!value) {
    return 0
  }

  const time = new Date(value).getTime()
  return Number.isNaN(time) ? 0 : time
}

// sortSeasonAnime returns a sorted copy. Only catalog-backed fields are
// available (no score/popularity), so sorting is limited to title, air date,
// and episode count.
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
    case 'title':
    default:
      return items.sort((left, right) =>
        titleCollator.compare(left.title ?? '', right.title ?? ''),
      )
  }
}
