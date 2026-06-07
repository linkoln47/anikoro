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

function isAbortError(error) {
  return error?.name === 'AbortError'
}

export default function useDashboardController() {
  const requestIdRef = useRef(0)
  const publicLoadAbortRef = useRef(null)
  const [dashboardUser, setDashboardUser] = useState(null)
  const [stats, setStats] = useState(emptyStats)
  const [anime, setAnime] = useState([])
  const [isLoading, setIsLoading] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [statusMessage, setStatusMessage] = useState('Checking MAL session...')

  const abortPublicDashboardLoad = useCallback(() => {
    publicLoadAbortRef.current?.abort()
    publicLoadAbortRef.current = null
  }, [])

  const cancelPublicDashboardLoad = useCallback(() => {
    requestIdRef.current += 1
    abortPublicDashboardLoad()
    setIsLoading(false)
  }, [abortPublicDashboardLoad])

  const clearDashboard = useCallback(() => {
    cancelPublicDashboardLoad()
    setDashboardUser(null)
    setStats(emptyStats)
    setAnime([])
  }, [cancelPublicDashboardLoad])

  const prepareDashboard = useCallback((nextDashboardUser) => {
    cancelPublicDashboardLoad()
    setDashboardUser(nextDashboardUser)
    setStats(emptyStats)
    setAnime([])
  }, [cancelPublicDashboardLoad])

  const loadSessionDashboard = useCallback(async (user) => {
    abortPublicDashboardLoad()

    if (!user) {
      setErrorMessage('Sign in with MAL first.')
      return
    }

    const requestId = requestIdRef.current + 1
    requestIdRef.current = requestId
    setDashboardUser({ mode: 'session', username: user.username })
    setIsLoading(true)
    setErrorMessage('')
    setStatusMessage(`Loading stats and anime for ${user.username}...`)

    try {
      const [nextStats, nextAnime] = await Promise.all([
        fetchStats(),
        fetchAnime(),
      ])

      if (requestIdRef.current !== requestId) {
        return
      }

      setStats(nextStats)
      setAnime(nextAnime)
      setStatusMessage(
        nextAnime.length > 0
          ? `Loaded ${nextAnime.length} grouped anime entries for ${user.username}.`
          : `${user.username} has no synced anime yet. Start a sync to fill the list.`,
      )
    } catch (error) {
      if (isAbortError(error)) {
        return
      }

      if (requestIdRef.current !== requestId) {
        return
      }

      setStats(emptyStats)
      setAnime([])
      setErrorMessage(error.message)
      setStatusMessage(`Could not load dashboard for ${user.username}.`)
    } finally {
      if (requestIdRef.current === requestId) {
        setIsLoading(false)
      }
    }
  }, [abortPublicDashboardLoad])

  const loadPublicDashboard = useCallback(async (username) => {
    let nextUsername
    try {
      nextUsername = parseMalUsername(username)
    } catch (error) {
      setErrorMessage(error.message)
      return
    }

    abortPublicDashboardLoad()
    const controller = new AbortController()
    publicLoadAbortRef.current = controller
    const requestId = requestIdRef.current + 1
    requestIdRef.current = requestId
    setDashboardUser({ mode: 'public', username: nextUsername })
    setIsLoading(true)
    setErrorMessage('')
    setStatusMessage(`Loading public list for ${nextUsername}...`)

    try {
      const [nextStats, nextAnime] = await Promise.all([
        fetchPublicStats(nextUsername, { signal: controller.signal }),
        fetchPublicAnime(nextUsername, { signal: controller.signal }),
      ])

      if (requestIdRef.current !== requestId) {
        return
      }

      setStats(nextStats)
      setAnime(nextAnime)
      setStatusMessage(
        nextAnime.length > 0
          ? `Loaded ${nextAnime.length} grouped anime entries for ${nextUsername}.`
          : `${nextUsername} has no synced public anime yet.`,
      )
    } catch (error) {
      if (isAbortError(error)) {
        return
      }

      if (requestIdRef.current !== requestId) {
        return
      }

      setStats(emptyStats)
      setAnime([])
      setErrorMessage(error.message)
      setStatusMessage(`Could not load public list for ${nextUsername}.`)
    } finally {
      if (publicLoadAbortRef.current === controller) {
        publicLoadAbortRef.current = null
      }

      if (requestIdRef.current === requestId) {
        setIsLoading(false)
      }
    }
  }, [abortPublicDashboardLoad])

  useEffect(() => {
    return () => {
      abortPublicDashboardLoad()
    }
  }, [abortPublicDashboardLoad])

  return {
    anime,
    cancelPublicDashboardLoad,
    clearDashboard,
    dashboardUser,
    errorMessage,
    isLoading,
    loadPublicDashboard,
    loadSessionDashboard,
    prepareDashboard,
    setErrorMessage,
    setStatusMessage,
    stats,
    statusMessage,
  }
}
