import { useEffect, useState } from 'react'
import { fetchAnime, fetchStats, startSync } from './api'
import AnimeListSection from './components/AnimeListSection'
import StatsGrid from './components/StatsGrid'
import StatusBlock from './components/StatusBlock'
import UserControls from './components/UserControls'

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

function normalizeUserId(value) {
  const normalized = value.trim()

  if (!/^[1-9]\d*$/.test(normalized)) {
    throw new Error('user_id must be a positive integer')
  }

  return normalized
}

function App() {
  const [userIdInput, setUserIdInput] = useState(readStoredUserId)
  const [activeUserId, setActiveUserId] = useState(readStoredUserId)
  const [stats, setStats] = useState(emptyStats)
  const [anime, setAnime] = useState([])
  const [isLoading, setIsLoading] = useState(false)
  const [isSyncing, setIsSyncing] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [statusMessage, setStatusMessage] = useState(
    activeUserId
      ? `Saved user #${activeUserId} found. Loading dashboard...`
      : 'Enter your internal app user id from PostgreSQL to load data.',
  )

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

  async function handleLoad(event) {
    event.preventDefault()

    try {
      const userId = normalizeUserId(userIdInput)
      persistUserId(userId)

      if (userId === activeUserId) {
        await loadDashboard(userId)
        return
      }

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
        setActiveUserId(userId)
        return
      }

      await loadDashboard(userId)
    } catch (error) {
      setErrorMessage(error.message)
    }
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
        <AnimeListSection
          activeUserId={activeUserId}
          anime={anime}
          isLoading={isLoading}
        />
      </section>
    </main>
  )
}

export default App
