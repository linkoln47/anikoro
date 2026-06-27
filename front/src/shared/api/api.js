import { apiUrl, request } from './client'
import {
  parseAccountUsername,
  parseSeasonName,
  parseSeasonYear,
  parseSyncJobId,
} from '../security/inputValidation'

export function authStartUrl() {
  return apiUrl('/api/auth/mal/start')
}

export function register({ email, username, password }) {
  return request('/api/auth/register', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ email, username, password }),
  })
}

export function login({ email, password }) {
  return request('/api/auth/login', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ email, password }),
  })
}

export function disconnectMal() {
  return request('/api/auth/mal/disconnect', {
    method: 'POST',
  })
}

export function fetchCurrentUser() {
  return request('/api/me')
}

export function logout() {
  return request('/api/auth/logout', {
    method: 'POST',
  })
}

export function fetchAnime() {
  return request('/api/anime')
}

export function fetchStats() {
  return request('/api/stats')
}

export function startSync() {
  return request('/api/sync', {
    method: 'POST',
  })
}

function syncJobPath(jobId) {
  return encodeURIComponent(parseSyncJobId(jobId))
}

export function fetchSyncJob(jobId) {
  return request(`/api/sync/jobs/${syncJobPath(jobId)}`)
}

export function syncJobEventsUrl(jobId) {
  return apiUrl(`/api/sync/jobs/${syncJobPath(jobId)}/events`)
}

export function updateAnimeListStatus(animeId, patch) {
  const id = Number.parseInt(animeId, 10)
  if (!Number.isInteger(id) || id <= 0) {
    return Promise.reject(new Error('Anime id must be a positive integer.'))
  }

  return request(`/api/anime/${id}/list-status`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(patch),
  })
}

export function removeAnimeListStatus(animeId) {
  const id = Number.parseInt(animeId, 10)
  if (!Number.isInteger(id) || id <= 0) {
    return Promise.reject(new Error('Anime id must be a positive integer.'))
  }

  return request(`/api/anime/${id}/list-status`, {
    method: 'DELETE',
  })
}

function publicUsernamePath(username) {
  return encodeURIComponent(parseAccountUsername(username))
}

export function fetchPublicAnime(username, options = {}) {
  return request(`/api/public/anime/${publicUsernamePath(username)}`, options)
}

export function fetchPublicStats(username, options = {}) {
  return request(`/api/public/stats/${publicUsernamePath(username)}`, options)
}

export function fetchFranchise(animeId, options = {}) {
  const id = Number.parseInt(animeId, 10)
  if (!Number.isInteger(id) || id <= 0) {
    return Promise.reject(new Error('Anime id must be a positive integer.'))
  }

  return request(`/api/franchise/${id}`, options)
}

// Fetches one page of the catalog-wide franchise grid. Filtering by media type,
// title, genres, and the R18+ gate, sorting, and paging all happen server-side, so
// the "All anime" page loads a window at a time instead of the whole catalog.
// Returns { items, total }.
export function fetchFranchises({
  mediaType,
  search,
  sort,
  genreIds,
  includeAdult,
  limit,
  offset,
  signal,
} = {}) {
  const params = new URLSearchParams()
  if (mediaType) {
    params.set('media_type', mediaType)
  }
  if (search) {
    params.set('q', search)
  }
  if (sort) {
    params.set('sort', sort)
  }
  if (Array.isArray(genreIds) && genreIds.length > 0) {
    params.set('genres', genreIds.join(','))
  }
  if (includeAdult) {
    params.set('adult', '1')
  }
  if (Number.isInteger(limit) && limit > 0) {
    params.set('limit', String(limit))
  }
  if (Number.isInteger(offset) && offset > 0) {
    params.set('offset', String(offset))
  }

  const queryString = params.toString()
  return request(`/api/franchises${queryString ? `?${queryString}` : ''}`, { signal })
}

// Fetches the catalog's genre universe for the "All anime" genre filter (which
// cannot derive its options from a single loaded page). Returns { genres }.
export function fetchGenres(options = {}) {
  return request('/api/genres', options)
}

export function fetchCurrentSeasonAnime(options = {}) {
  return request('/api/season', options)
}

export function fetchSeasonAnime(year, season, options = {}) {
  const validYear = parseSeasonYear(year)
  const validSeason = parseSeasonName(season)

  return request(`/api/season/${validYear}/${validSeason}`, options)
}
