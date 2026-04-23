const apiBaseUrl = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')

async function request(path, options = {}) {
  const { headers, ...fetchOptions } = options

  const response = await fetch(`${apiBaseUrl}${path}`, {
    credentials: 'include',
    ...fetchOptions,
    headers: {
      Accept: 'application/json',
      ...headers,
    },
  })

  const contentType = response.headers.get('content-type') ?? ''
  const payload = contentType.includes('application/json')
    ? await response.json()
    : await response.text()

  if (!response.ok) {
    const message =
      typeof payload === 'string' && payload.trim().length > 0
        ? payload.trim()
        : `Request failed with status ${response.status}`
    throw new Error(message)
  }

  return payload
}

export function authStartUrl() {
  return `${apiBaseUrl}/api/auth/mal/start`
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
  return encodeURIComponent(jobId.trim())
}

export function fetchSyncJob(jobId) {
  return request(`/api/sync/jobs/${syncJobPath(jobId)}`)
}

export function syncJobEventsUrl(jobId) {
  return `${apiBaseUrl}/api/sync/jobs/${syncJobPath(jobId)}/events`
}

function publicUsernamePath(username) {
  return encodeURIComponent(username.trim())
}

export function fetchPublicAnime(username) {
  return request(`/api/public/anime/${publicUsernamePath(username)}`)
}

export function fetchPublicStats(username) {
  return request(`/api/public/stats/${publicUsernamePath(username)}`)
}

export function startPublicSync(username) {
  return request('/api/public/sync', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ username: username.trim() }),
  })
}
