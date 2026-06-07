import { apiUrl, request } from './client'
import { parseMalUsername, parseSyncJobId } from '../security/inputValidation'

export function authStartUrl() {
  return apiUrl('/api/auth/mal/start')
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

function publicUsernamePath(username) {
  return encodeURIComponent(parseMalUsername(username))
}

export function fetchPublicAnime(username, options = {}) {
  return request(`/api/public/anime/${publicUsernamePath(username)}`, options)
}

export function fetchPublicStats(username, options = {}) {
  return request(`/api/public/stats/${publicUsernamePath(username)}`, options)
}

export function startPublicSync(username) {
  const validUsername = parseMalUsername(username)

  return request('/api/public/sync', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ username: validUsername }),
  })
}
