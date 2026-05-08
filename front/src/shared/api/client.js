export const apiBaseUrl = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '')

export function apiUrl(path) {
  return `${apiBaseUrl}${path}`
}

export async function request(path, options = {}) {
  const { headers, ...fetchOptions } = options

  const response = await fetch(apiUrl(path), {
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
