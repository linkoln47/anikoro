import { useEffect, useRef, useState } from 'react'
import {
  authStartUrl,
  fetchAnime,
  fetchCurrentUser,
  fetchStats,
  logout,
  startSync,
} from './api'
import AnimeDetailsSection from './components/AnimeDetailsSection'
import AnimeListSection from './components/AnimeListSection'
import StatsGrid from './components/StatsGrid'
import StatusBlock from './components/StatusBlock'
import UserControls from './components/UserControls'
import useScrollBackground from './useScrollBackground'

const emptyStats = {
  series_count: 0,
  movies_count: 0,
  total_count: 0,
}

function readSelectedAnimeId() {
  if (typeof window === 'undefined') {
    return null
  }

  const match = window.location.hash.match(/^#\/anime\/([1-9]\d*)$/)
  if (!match) {
    return null
  }

  return Number(match[1])
}

function openAnimeRoute(animeId) {
  if (typeof window === 'undefined') {
    return
  }

  window.location.hash = `/anime/${animeId}`
}

function clearAnimeRoute() {
  if (typeof window === 'undefined') {
    return
  }

  window.history.replaceState(
    null,
    '',
    `${window.location.pathname}${window.location.search}`,
  )
}

function App() {
  useScrollBackground()

  const listRegionRef = useRef(null)
  const shouldRestoreListFocusRef = useRef(false)
  const [currentUser, setCurrentUser] = useState(null)
  const [selectedAnimeId, setSelectedAnimeId] = useState(readSelectedAnimeId)
  const [stats, setStats] = useState(emptyStats)
  const [anime, setAnime] = useState([])
  const [isCheckingSession, setIsCheckingSession] = useState(true)
  const [isLoading, setIsLoading] = useState(false)
  const [isSyncing, setIsSyncing] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [statusMessage, setStatusMessage] = useState('Checking MAL session...')
  const isDetailsOpen = selectedAnimeId !== null
  const activeUsername = currentUser?.username ?? ''

  async function loadDashboard(user = currentUser) {
    if (!user) {
      setErrorMessage('Sign in with MAL first.')
      return
    }

    setIsLoading(true)
    setErrorMessage('')
    setStatusMessage(`Loading stats and anime for ${user.username}...`)

    try {
      const [nextStats, nextAnime] = await Promise.all([
        fetchStats(),
        fetchAnime(),
      ])

      setStats(nextStats)
      setAnime(nextAnime)
      setStatusMessage(
        nextAnime.length > 0
          ? `Loaded ${nextAnime.length} grouped anime entries for ${user.username}.`
          : `${user.username} has no synced anime yet. Start a sync to fill the list.`,
      )
    } catch (error) {
      setStats(emptyStats)
      setAnime([])
      setErrorMessage(error.message)
      setStatusMessage(`Could not load dashboard for ${user.username}.`)
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    async function loadSession() {
      setIsCheckingSession(true)
      setErrorMessage('')

      try {
        const response = await fetchCurrentUser()
        if (!response.authenticated || !response.user) {
          throw new Error('No active MAL session')
        }

        setCurrentUser(response.user)
        setStatusMessage(`Signed in as ${response.user.username}. Loading dashboard...`)
        void loadDashboard(response.user)
      } catch {
        setCurrentUser(null)
        setStats(emptyStats)
        setAnime([])
        setStatusMessage('Sign in with MAL to load your dashboard.')
      } finally {
        setIsCheckingSession(false)
      }
    }

    void loadSession()
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') {
      return undefined
    }

    function handleHashChange() {
      setSelectedAnimeId(readSelectedAnimeId())
    }

    window.addEventListener('hashchange', handleHashChange)

    return () => {
      window.removeEventListener('hashchange', handleHashChange)
    }
  }, [])

  useEffect(() => {
    if (isDetailsOpen) {
      return
    }

    if (!shouldRestoreListFocusRef.current) {
      return
    }

    listRegionRef.current?.focus()
    shouldRestoreListFocusRef.current = false
  }, [isDetailsOpen])

  function handleLogin() {
    if (typeof window === 'undefined') {
      return
    }

    window.location.assign(authStartUrl())
  }

  async function handleLogout() {
    try {
      setErrorMessage('')
      await logout()
    } catch (error) {
      setErrorMessage(error.message)
    } finally {
      clearAnimeRoute()
      setSelectedAnimeId(null)
      setCurrentUser(null)
      setStats(emptyStats)
      setAnime([])
      setStatusMessage('Signed out. Sign in with MAL to load your dashboard.')
    }
  }

  async function handleSync() {
    if (!currentUser) {
      setErrorMessage('Sign in with MAL first.')
      return
    }

    try {
      setIsSyncing(true)
      setErrorMessage('')

      const response = await startSync()
      setStatusMessage(`${response.message}. Refresh after a few seconds.`)
    } catch (error) {
      setErrorMessage(error.message)
    } finally {
      setIsSyncing(false)
    }
  }

  async function handleRefresh() {
    await loadDashboard()
  }

  function handleAnimeSelect(animeId) {
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur()
    }

    setSelectedAnimeId(animeId)
    openAnimeRoute(animeId)
  }

  function handleAnimeBack() {
    shouldRestoreListFocusRef.current = true
    clearAnimeRoute()
    setSelectedAnimeId(null)
  }

  return (
    <main className="app-shell">
      <section className="dashboard">
        <header className="hero-card">
          <p className="eyebrow">MAL Dashboard</p>
          <h1>Frontend connected to your Go API</h1>
          <p className="lead">
            Sign in with MAL once, then the UI talks to <code>/api/me</code>,{' '}
            <code>/api/anime</code>, <code>/api/stats</code>, and{' '}
            <code>/api/sync</code> through the Vite dev proxy.
          </p>
        </header>

        <section className="panel control-panel">
          {/* Session actions */}
          <UserControls
            currentUser={currentUser}
            onLogin={handleLogin}
            onLogout={handleLogout}
            onSync={handleSync}
            onRefresh={handleRefresh}
            isCheckingSession={isCheckingSession}
            isLoading={isLoading}
            isSyncing={isSyncing}
          />

          {/* Feedback for the current request state */}
          <StatusBlock
            statusMessage={statusMessage}
            errorMessage={errorMessage}
          />
        </section>

        {/* Aggregate totals */}
        <StatsGrid stats={stats} isLoading={isLoading || isCheckingSession} />

        {/* Loaded anime entries */}
        <div
          ref={listRegionRef}
          className="list-region-shell"
          tabIndex={-1}
          hidden={isDetailsOpen}
          inert={isDetailsOpen ? '' : undefined}
        >
          <AnimeListSection
            activeUsername={activeUsername}
            anime={anime}
            isLoading={isLoading || isCheckingSession}
            onSelectAnime={handleAnimeSelect}
          />
        </div>

        {isDetailsOpen ? (
          <AnimeDetailsSection
            activeUsername={activeUsername}
            anime={anime}
            selectedAnimeId={selectedAnimeId}
            isLoading={isLoading || isCheckingSession}
            onBack={handleAnimeBack}
          />
        ) : null}
      </section>
    </main>
  )
}

export default App
