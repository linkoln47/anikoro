const apiBaseUrl = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')

async function request(path, options = {}) {
  const response = await fetch(`${apiBaseUrl}${path}`, {
    headers: {
      Accept: 'application/json',
      ...options.headers,
    },
    ...options,
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

export function fetchAnime(userId) {
  return request(`/api/anime/${userId}`)
}

export function fetchStats(userId) {
  return request(`/api/stats/${userId}`)
}

export function startSync(userId) {
  return request(`/api/sync/${userId}`, {
    method: 'POST',
  })
}
