import { useCallback, useRef, useState } from 'react'
import {
  fetchAnime,
  fetchPublicAnime,
  fetchPublicStats,
  fetchStats,
} from '../../shared/api/malApi'

const emptyStats = {
  series_count: 0,
  movies_count: 0,
  total_count: 0,
}

export default function useDashboardController() {
  const requestIdRef = useRef(0)
  const [dashboardUser, setDashboardUser] = useState(null)
  const [stats, setStats] = useState(emptyStats)
  const [anime, setAnime] = useState([])
  const [isLoading, setIsLoading] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [statusMessage, setStatusMessage] = useState('Checking MAL session...')

  const clearDashboard = useCallback(() => {
    requestIdRef.current += 1
    setDashboardUser(null)
    setStats(emptyStats)
    setAnime([])
    setIsLoading(false)
  }, [])

  const prepareDashboard = useCallback((nextDashboardUser) => {
    requestIdRef.current += 1
    setDashboardUser(nextDashboardUser)
    setStats(emptyStats)
    setAnime([])
    setIsLoading(false)
  }, [])

  const loadSessionDashboard = useCallback(async (user) => {
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
  }, [])

  const loadPublicDashboard = useCallback(async (username) => {
    const nextUsername = username.trim()
    if (!nextUsername) {
      setErrorMessage('Enter a MAL username.')
      return
    }

    const requestId = requestIdRef.current + 1
    requestIdRef.current = requestId
    setDashboardUser({ mode: 'public', username: nextUsername })
    setIsLoading(true)
    setErrorMessage('')
    setStatusMessage(`Loading public list for ${nextUsername}...`)

    try {
      const [nextStats, nextAnime] = await Promise.all([
        fetchPublicStats(nextUsername),
        fetchPublicAnime(nextUsername),
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
      if (requestIdRef.current !== requestId) {
        return
      }

      setStats(emptyStats)
      setAnime([])
      setErrorMessage(error.message)
      setStatusMessage(`Could not load public list for ${nextUsername}.`)
    } finally {
      if (requestIdRef.current === requestId) {
        setIsLoading(false)
      }
    }
  }, [])

  return {
    anime,
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
