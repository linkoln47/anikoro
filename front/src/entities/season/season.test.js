import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  collectSeasonGenres,
  filterOutExplicitAnime,
  filterOutExplicitGenres,
  filterSeasonAnimeByGenres,
  filterSeasonAnimeByMediaType,
  getAdjacentSeason,
  getCurrentSeason,
  groupSeasonGenres,
  hasExplicitGenre,
  isValidSeasonName,
  sortSeasonAnime,
} from './season.js'

describe('getCurrentSeason', () => {
  it('maps months to MAL season boundaries', () => {
    assert.deepEqual(getCurrentSeason(new Date(2026, 0, 15)), { year: 2026, season: 'winter' })
    assert.deepEqual(getCurrentSeason(new Date(2026, 3, 1)), { year: 2026, season: 'spring' })
    assert.deepEqual(getCurrentSeason(new Date(2026, 6, 31)), { year: 2026, season: 'summer' })
    assert.deepEqual(getCurrentSeason(new Date(2026, 11, 31)), { year: 2026, season: 'fall' })
  })
})

describe('getAdjacentSeason', () => {
  it('moves to the next season within the same year', () => {
    assert.deepEqual(getAdjacentSeason({ year: 2026, season: 'winter' }, 1), {
      year: 2026,
      season: 'spring',
    })
  })

  it('wraps the year backwards from winter to fall', () => {
    assert.deepEqual(getAdjacentSeason({ year: 2026, season: 'winter' }, -1), {
      year: 2025,
      season: 'fall',
    })
  })

  it('wraps the year forwards from fall to winter', () => {
    assert.deepEqual(getAdjacentSeason({ year: 2026, season: 'fall' }, 1), {
      year: 2027,
      season: 'winter',
    })
  })
})

describe('isValidSeasonName', () => {
  it('accepts only the four season names', () => {
    assert.equal(isValidSeasonName('summer'), true)
    assert.equal(isValidSeasonName('autumn'), false)
  })
})

describe('sortSeasonAnime', () => {
  const anime = [
    { id: 1, title: 'Beta', start_date: '2026-07-10', num_episodes: 12 },
    { id: 2, title: 'Alpha', start_date: '2026-09-01', num_episodes: 24 },
  ]

  it('sorts by title alphabetically by default', () => {
    assert.deepEqual(
      sortSeasonAnime(anime, 'title').map((item) => item.title),
      ['Alpha', 'Beta'],
    )
  })

  it('sorts by newest air date first', () => {
    assert.deepEqual(
      sortSeasonAnime(anime, 'date').map((item) => item.id),
      [2, 1],
    )
  })

  it('sorts by episode count descending', () => {
    assert.deepEqual(
      sortSeasonAnime(anime, 'episodes').map((item) => item.id),
      [2, 1],
    )
  })

  it('does not mutate the input array', () => {
    const input = [...anime]
    sortSeasonAnime(input, 'title')
    assert.deepEqual(input, anime)
  })
})

describe('collectSeasonGenres', () => {
  const anime = [
    { id: 1, genres: [{ id: 4, name: 'Comedy' }, { id: 1, name: 'Action' }] },
    { id: 2, genres: [{ id: 1, name: 'Action' }, { id: 2, name: 'Drama' }] },
    { id: 3 },
  ]

  it('returns unique genres sorted by name', () => {
    assert.deepEqual(collectSeasonGenres(anime), [
      { id: 1, name: 'Action' },
      { id: 4, name: 'Comedy' },
      { id: 2, name: 'Drama' },
    ])
  })

  it('returns an empty array for no anime', () => {
    assert.deepEqual(collectSeasonGenres([]), [])
  })
})

describe('filterSeasonAnimeByGenres', () => {
  const anime = [
    { id: 1, genres: [{ id: 1, name: 'Action' }, { id: 4, name: 'Comedy' }] },
    { id: 2, genres: [{ id: 1, name: 'Action' }] },
    { id: 3, genres: [] },
  ]

  it('returns all anime when no genre is selected', () => {
    assert.deepEqual(filterSeasonAnimeByGenres(anime, []).map((item) => item.id), [1, 2, 3])
  })

  it('keeps only anime carrying every selected genre (AND)', () => {
    assert.deepEqual(filterSeasonAnimeByGenres(anime, [1]).map((item) => item.id), [1, 2])
    assert.deepEqual(filterSeasonAnimeByGenres(anime, [1, 4]).map((item) => item.id), [1])
  })

  it('returns empty when no anime matches', () => {
    assert.deepEqual(filterSeasonAnimeByGenres(anime, [999]), [])
  })
})

describe('hasExplicitGenre / filterOutExplicitAnime', () => {
  const anime = [
    { id: 1, genres: [{ id: 1, name: 'Action' }, { id: 4, name: 'Comedy' }] },
    { id: 2, genres: [{ id: 9, name: 'Ecchi' }] },
    { id: 3, genres: [{ id: 1, name: 'Action' }, { id: 12, name: 'hentai' }] },
    { id: 4, genres: [{ id: 49, name: 'EROTICA' }] },
    { id: 5, genres: [] },
    { id: 6 },
  ]

  it('flags anime carrying an explicit genre regardless of case', () => {
    assert.equal(hasExplicitGenre(anime[1]), true)
    assert.equal(hasExplicitGenre(anime[2]), true)
    assert.equal(hasExplicitGenre(anime[3]), true)
    assert.equal(hasExplicitGenre(anime[0]), false)
    assert.equal(hasExplicitGenre(anime[4]), false)
    assert.equal(hasExplicitGenre(anime[5]), false)
  })

  it('drops anime tagged Ecchi/Hentai/Erotica and keeps the rest', () => {
    assert.deepEqual(filterOutExplicitAnime(anime).map((item) => item.id), [1, 5, 6])
  })

  it('returns an empty array for non-array input', () => {
    assert.deepEqual(filterOutExplicitAnime(null), [])
    assert.deepEqual(filterOutExplicitAnime(undefined), [])
  })
})

describe('groupSeasonGenres', () => {
  it('buckets genres into ordered sections, Themes catching the rest', () => {
    const genres = [
      { id: 1, name: 'Action' },
      { id: 18, name: 'Mecha' },
      { id: 27, name: 'Shounen' },
      { id: 9, name: 'Ecchi' },
      { id: 4, name: 'Comedy' },
    ]

    const sections = groupSeasonGenres(genres)
    assert.deepEqual(
      sections.map((section) => [section.label, section.genres.map((genre) => genre.name)]),
      [
        ['Genres', ['Action', 'Comedy']],
        ['Explicit Genres', ['Ecchi']],
        ['Themes', ['Mecha']],
        ['Demographics', ['Shounen']],
      ],
    )
  })

  it('omits empty sections and returns [] for non-array input', () => {
    const sections = groupSeasonGenres([{ id: 1, name: 'Action' }])
    assert.deepEqual(sections.map((section) => section.label), ['Genres'])
    assert.deepEqual(groupSeasonGenres(null), [])
  })
})

describe('filterOutExplicitGenres', () => {
  it('drops explicit genre options regardless of case', () => {
    const genres = [
      { id: 1, name: 'Action' },
      { id: 9, name: 'Ecchi' },
      { id: 12, name: 'hentai' },
      { id: 49, name: 'EROTICA' },
    ]
    assert.deepEqual(filterOutExplicitGenres(genres).map((genre) => genre.id), [1])
  })

  it('returns [] for non-array input', () => {
    assert.deepEqual(filterOutExplicitGenres(undefined), [])
  })
})

describe('filterSeasonAnimeByMediaType', () => {
  const anime = [
    { id: 1, media_type: 'tv' },
    { id: 2, media_type: 'movie' },
    { id: 3, media_type: 'TV' },
    { id: 4 },
  ]

  it('returns all anime when no media type is selected', () => {
    assert.deepEqual(filterSeasonAnimeByMediaType(anime, '').map((item) => item.id), [1, 2, 3, 4])
  })

  it('keeps only anime of the selected media type (case-insensitive)', () => {
    assert.deepEqual(filterSeasonAnimeByMediaType(anime, 'tv').map((item) => item.id), [1, 3])
    assert.deepEqual(filterSeasonAnimeByMediaType(anime, 'movie').map((item) => item.id), [2])
  })

  it('returns [] for non-array input', () => {
    assert.deepEqual(filterSeasonAnimeByMediaType(null, 'tv'), [])
  })
})
