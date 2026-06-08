import { useCallback, useEffect, useRef, useState } from 'react'
import {
  fetchAnime,
  fetchPublicAnime,
  fetchPublicStats,
  fetchStats,
} from '../../shared/api/malApi'
import { parseMalUsername } from '../../shared/security/inputValidation'

const emptyStats = {
  series_count: 0,
  movies_count: 0,
  total_count: 0,
}

function createEmptyDashboard() {
  return {
    user: null,
    stats: emptyStats,
    anime: [],
    isLoading: false,
    error: '',
  }
}

function isAbortError(error) {
  return error?.name === 'AbortError'
}

export default function useDashboardController() {
  const sessionRequestIdRef = useRef(0)
  const publicRequestIdRef = useRef(0)
  const publicLoadAbortRef = useRef(null)
  const [activeDashboardMode, setActiveDashboardMode] = useState(null)
  const [sessionDashboard, setSessionDashboard] = useState(createEmptyDashboard)
  const [publicDashboard, setPublicDashboard] = useState(createEmptyDashboard)
  const [errorMessage, setErrorMessage] = useState('')
  const [statusMessage, setStatusMessage] = useState('Checking MAL session...')

  const abortPublicDashboardLoad = useCallback(() => {
    publicLoadAbortRef.current?.abort()
    publicLoadAbortRef.current = null
  }, [])

  const cancelPublicDashboardLoad = useCallback(() => {
    publicRequestIdRef.current += 1
    abortPublicDashboardLoad()
    setPublicDashboard((current) => ({
      ...current,
      isLoading: false,
    }))
  }, [abortPublicDashboardLoad])

  const clearSessionDashboard = useCallback(() => {
    sessionRequestIdRef.current += 1
    setSessionDashboard(createEmptyDashboard())
  }, [])

  const clearPublicDashboard = useCallback(() => {
    cancelPublicDashboardLoad()
    setPublicDashboard(createEmptyDashboard())
  }, [cancelPublicDashboardLoad])

  const clearDashboard = useCallback(() => {
    clearSessionDashboard()
    clearPublicDashboard()
    setActiveDashboardMode(null)
  }, [clearPublicDashboard, clearSessionDashboard])

  const prepareSessionDashboard = useCallback((user, options = {}) => {
    sessionRequestIdRef.current += 1

    if (options.activate !== false) {
      setActiveDashboardMode('session')
    }

    setSessionDashboard({
      user,
      stats: emptyStats,
      anime: [],
      isLoading: false,
      error: '',
    })
  }, [])

  const preparePublicDashboard = useCallback((username, options = {}) => {
    let nextUsername
    try {
      nextUsername = parseMalUsername(username)
    } catch (error) {
      setErrorMessage(error.message)
      return
    }

    cancelPublicDashboardLoad()

    if (options.activate !== false) {
      setActiveDashboardMode('public')
    }

    setPublicDashboard({
      user: { username: nextUsername },
      stats: emptyStats,
      anime: [],
      isLoading: false,
      error: '',
    })
  }, [cancelPublicDashboardLoad])

  const hydrateSessionDashboard = useCallback((user, snapshot) => {
    if (!user || !snapshot) {
      return
    }

    setSessionDashboard((current) => {
      if (current.isLoading) {
        return current
      }

      return {
        user,
        stats: snapshot.stats ?? emptyStats,
        anime: Array.isArray(snapshot.anime) ? snapshot.anime : [],
        isLoading: false,
        error: '',
      }
    })
  }, [])

  const loadSessionDashboard = useCallback(async (user, options = {}) => {
    const shouldActivate = options.activate !== false

    if (!user) {
      setSessionDashboard({
        user: null,
        stats: emptyStats,
        anime: [],
        isLoading: false,
        error: 'Sign in with MAL first.',
      })
      setErrorMessage('Sign in with MAL first.')
      return null
    }

    const requestId = sessionRequestIdRef.current + 1
    sessionRequestIdRef.current = requestId

    if (shouldActivate) {
      setActiveDashboardMode('session')
    }

    setSessionDashboard({
      user,
      stats: emptyStats,
      anime: [],
      isLoading: true,
      error: '',
    })
    setErrorMessage('')
    setStatusMessage(`Loading stats and anime for ${user.username}...`)

    try {
      const [nextStats, nextAnime] = await Promise.all([
        fetchStats(),
        fetchAnime(),
      ])

      if (sessionRequestIdRef.current !== requestId) {
        return null
      }

      const snapshot = {
        username: user.username,
        stats: nextStats,
        anime: nextAnime,
      }

      setSessionDashboard({
        user,
        stats: nextStats,
        anime: nextAnime,
        isLoading: false,
        error: '',
      })
      setStatusMessage(
        nextAnime.length > 0
          ? `Loaded ${nextAnime.length} grouped anime entries for ${user.username}.`
          : `${user.username} has no synced anime yet. Start a sync to fill the list.`,
      )
      return snapshot
    } catch (error) {
      if (isAbortError(error)) {
        return null
      }

      if (sessionRequestIdRef.current !== requestId) {
        return null
      }

      setSessionDashboard({
        user,
        stats: emptyStats,
        anime: [],
        isLoading: false,
        error: error.message,
      })
      setErrorMessage(error.message)
      setStatusMessage(`Could not load dashboard for ${user.username}.`)
      return null
    } finally {
      if (sessionRequestIdRef.current === requestId) {
        setSessionDashboard((current) => ({
          ...current,
          isLoading: false,
        }))
      }
    }
  }, [])

  const loadPublicDashboard = useCallback(async (username, options = {}) => {
    const shouldActivate = options.activate !== false
    let nextUsername
    try {
      nextUsername = parseMalUsername(username)
    } catch (error) {
      setErrorMessage(error.message)
      return null
    }

    abortPublicDashboardLoad()
    const controller = new AbortController()
    publicLoadAbortRef.current = controller
    const requestId = publicRequestIdRef.current + 1
    publicRequestIdRef.current = requestId

    if (shouldActivate) {
      setActiveDashboardMode('public')
    }

    setPublicDashboard({
      user: { username: nextUsername },
      stats: emptyStats,
      anime: [],
      isLoading: true,
      error: '',
    })
    setErrorMessage('')
    setStatusMessage(`Loading public list for ${nextUsername}...`)

    try {
      const [nextStats, nextAnime] = await Promise.all([
        fetchPublicStats(nextUsername, { signal: controller.signal }),
        fetchPublicAnime(nextUsername, { signal: controller.signal }),
      ])

      if (publicRequestIdRef.current !== requestId) {
        return null
      }

      const snapshot = {
        username: nextUsername,
        stats: nextStats,
        anime: nextAnime,
      }

      setPublicDashboard({
        user: { username: nextUsername },
        stats: nextStats,
        anime: nextAnime,
        isLoading: false,
        error: '',
      })
      setStatusMessage(
        nextAnime.length > 0
          ? `Loaded ${nextAnime.length} grouped anime entries for ${nextUsername}.`
          : `${nextUsername} has no synced public anime yet.`,
      )
      return snapshot
    } catch (error) {
      if (isAbortError(error)) {
        return null
      }

      if (publicRequestIdRef.current !== requestId) {
        return null
      }

      setPublicDashboard({
        user: { username: nextUsername },
        stats: emptyStats,
        anime: [],
        isLoading: false,
        error: error.message,
      })
      setErrorMessage(error.message)
      setStatusMessage(`Could not load public list for ${nextUsername}.`)
      return null
    } finally {
      if (publicLoadAbortRef.current === controller) {
        publicLoadAbortRef.current = null
      }

      if (publicRequestIdRef.current === requestId) {
        setPublicDashboard((current) => ({
          ...current,
          isLoading: false,
        }))
      }
    }
  }, [abortPublicDashboardLoad])

  useEffect(() => {
    return () => {
      abortPublicDashboardLoad()
    }
  }, [abortPublicDashboardLoad])

  return {
    activeDashboardMode,
    cancelPublicDashboardLoad,
    clearDashboard,
    clearPublicDashboard,
    clearSessionDashboard,
    errorMessage,
    hydrateSessionDashboard,
    loadPublicDashboard,
    loadSessionDashboard,
    preparePublicDashboard,
    prepareSessionDashboard,
    publicDashboard,
    sessionDashboard,
    setErrorMessage,
    setStatusMessage,
    statusMessage,
  }
}
