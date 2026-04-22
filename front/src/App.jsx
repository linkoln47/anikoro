import { useEffect, useRef, useState } from 'react'
import { fetchAnime, fetchStats, startSync } from './api'
import AnimeDetailsSection from './components/AnimeDetailsSection'
import AnimeListSection from './components/AnimeListSection'
import StatsGrid from './components/StatsGrid'
import StatusBlock from './components/StatusBlock'
import UserControls from './components/UserControls'
import useScrollBackground from './useScrollBackground'

const storageKey = 'mal.front.userId'
const emptyStats = {
  series_count: 0,
  movies_count: 0,
  total_count: 0,
}

function readStoredUserId() {
  if (typeof window === 'undefined') {
    return ''
  }

  return window.localStorage.getItem(storageKey) ?? ''
}

function persistUserId(userId) {
  if (typeof window === 'undefined') {
    return
  }

  window.localStorage.setItem(storageKey, userId)
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

function normalizeUserId(value) {
  const normalized = value.trim()

  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error('user_id must be a positive integer')
  }

  return normalized
}

function App() {
  useScrollBackground()

  const listRegionRef = useRef(null)
  const shouldRestoreListFocusRef = useRef(false)
  const [userIdInput, setUserIdInput] = useState(readStoredUserId)
  const [activeUserId, setActiveUserId] = useState(readStoredUserId)
  const [selectedAnimeId, setSelectedAnimeId] = useState(readSelectedAnimeId)
  const [stats, setStats] = useState(emptyStats)
  const [anime, setAnime] = useState([])
  const [isLoading, setIsLoading] = useState(() => Boolean(readStoredUserId()))
  const [isSyncing, setIsSyncing] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [statusMessage, setStatusMessage] = useState(
    activeUserId
      ? `Saved user #${activeUserId} found. Loading dashboard...`
      : 'Enter your internal app user id from PostgreSQL to load data.',
  )
  const isDetailsOpen = selectedAnimeId !== null

  async function loadDashboard(userId) {
    setIsLoading(true)
    setErrorMessage('')
    setStatusMessage(`Loading stats and anime for user #${userId}...`)

    try {
      const [nextStats, nextAnime] = await Promise.all([
        fetchStats(userId),
        fetchAnime(userId),
      ])

      setStats(nextStats)
      setAnime(nextAnime)
      setStatusMessage(
        nextAnime.length > 0
          ? `Loaded ${nextAnime.length} grouped anime entries for user #${userId}.`
          : `User #${userId} has no synced anime yet. Start a sync to fill the list.`,
      )
    } catch (error) {
      setStats(emptyStats)
      setAnime([])
      setErrorMessage(error.message)
      setStatusMessage(`Could not load dashboard for user #${userId}.`)
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    if (!activeUserId) {
      return
    }

    void loadDashboard(activeUserId)
  }, [activeUserId])

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

  async function handleLoad(event) {
    event.preventDefault()

    try {
      const userId = normalizeUserId(userIdInput)
      persistUserId(userId)

      if (userId === activeUserId) {
        await loadDashboard(userId)
        return
      }

      clearAnimeRoute()
      setSelectedAnimeId(null)
      setActiveUserId(userId)
    } catch (error) {
      setErrorMessage(error.message)
    }
  }

  async function handleSync() {
    try {
      const userId = normalizeUserId(userIdInput)
      persistUserId(userId)
      setIsSyncing(true)
      setErrorMessage('')

      const response = await startSync(userId)
      setStatusMessage(`${response.message}. Refresh after a few seconds.`)

      if (userId !== activeUserId) {
        clearAnimeRoute()
        setSelectedAnimeId(null)
        setActiveUserId(userId)
      }
    } catch (error) {
      setErrorMessage(error.message)
    } finally {
      setIsSyncing(false)
    }
  }

  async function handleRefresh() {
    try {
      const userId = normalizeUserId(userIdInput || activeUserId)
      persistUserId(userId)

      if (userId !== activeUserId) {
        clearAnimeRoute()
        setSelectedAnimeId(null)
        setActiveUserId(userId)
        return
      }

      await loadDashboard(userId)
    } catch (error) {
      setErrorMessage(error.message)
    }
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
            The UI talks to <code>/api/anime/:user_id</code>,{' '}
            <code>/api/stats/:user_id</code>, and <code>/api/sync/:user_id</code>{' '}
            through the Vite dev proxy.
          </p>
        </header>

        <section className="panel control-panel">
          {/* User lookup and dashboard actions */}
          <UserControls
            userIdInput={userIdInput}
            onUserIdChange={setUserIdInput}
            onLoad={handleLoad}
            onSync={handleSync}
            onRefresh={handleRefresh}
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
        <StatsGrid stats={stats} isLoading={isLoading} />

        {/* Loaded anime entries */}
        <div
          ref={listRegionRef}
          className="list-region-shell"
          tabIndex={-1}
          hidden={isDetailsOpen}
          inert={isDetailsOpen ? '' : undefined}
        >
          <AnimeListSection
            activeUserId={activeUserId}
            anime={anime}
            isLoading={isLoading}
            onSelectAnime={handleAnimeSelect}
          />
        </div>

        {isDetailsOpen ? (
          <AnimeDetailsSection
            activeUserId={activeUserId}
            anime={anime}
            selectedAnimeId={selectedAnimeId}
            isLoading={isLoading}
            onBack={handleAnimeBack}
          />
        ) : null}
      </section>
    </main>
  )
}

export default App
