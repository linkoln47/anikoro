import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  getAdjacentSeason,
  getCurrentSeason,
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
