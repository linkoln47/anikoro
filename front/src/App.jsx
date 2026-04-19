import { useEffect, useState } from 'react'
import { fetchAnime, fetchStats, startSync } from './api'

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

function formatSyncedAt(value) {
  if (!value) {
    return 'n/a'
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }

  return new Intl.DateTimeFormat('en', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

function formatScore(value) {
  const numeric = Number(value)
  if (Number.isNaN(numeric)) {
    return 'n/a'
  }

  return Number.isInteger(numeric) ? numeric.toFixed(0) : numeric.toFixed(1)
}

function formatTypeLabel(value) {
  if (value === 'series') {
    return 'Series'
  }

  if (value === 'movie') {
    return 'Movie'
  }

  return value
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
          <form className="controls" onSubmit={handleLoad}>
            <label className="field">
              <span className="field-label">App user id</span>
              <input
                className="text-input"
                type="text"
                inputMode="numeric"
                placeholder="Example: 1"
                value={userIdInput}
                onChange={(event) => setUserIdInput(event.target.value)}
              />
            </label>

            <div className="action-row">
              <button className="primary-button" type="submit" disabled={isLoading}>
                {isLoading ? 'Loading...' : 'Load Data'}
              </button>
              <button
                className="secondary-button"
                type="button"
                onClick={handleSync}
                disabled={isSyncing}
              >
                {isSyncing ? 'Starting...' : 'Start Sync'}
              </button>
              <button
                className="ghost-button"
                type="button"
                onClick={handleRefresh}
                disabled={isLoading}
              >
                Refresh
              </button>
            </div>
          </form>

          <div className="status-block">
            <p className="status-message">{statusMessage}</p>
            <p className="hint">
              Use the internal <code>users.id</code> from PostgreSQL, not the MAL
              username.
            </p>
            {errorMessage ? <p className="error-banner">{errorMessage}</p> : null}
          </div>
        </section>

        <section className="stats-grid">
          <article className="panel stat-card">
            <span className="stat-label">Series</span>
            <strong>{stats.series_count}</strong>
          </article>
          <article className="panel stat-card">
            <span className="stat-label">Movies</span>
            <strong>{stats.movies_count}</strong>
          </article>
          <article className="panel stat-card">
            <span className="stat-label">Total</span>
            <strong>{stats.total_count}</strong>
          </article>
        </section>

        <section className="panel list-panel">
          <div className="section-heading">
            <div>
              <p className="section-eyebrow">Anime List</p>
              <h2>{activeUserId ? `User #${activeUserId}` : 'No user selected'}</h2>
            </div>
            <span className="list-meta">{anime.length} entries</span>
          </div>

          {!activeUserId ? (
            <div className="empty-state">
              Enter a user id and click <strong>Load Data</strong>.
            </div>
          ) : isLoading ? (
            <div className="empty-state">Loading anime list...</div>
          ) : anime.length === 0 ? (
            <div className="empty-state">
              No grouped anime entries yet. Run sync and refresh this page.
            </div>
          ) : (
            <div className="anime-table-shell">
              <table className="anime-table">
                <thead>
                  <tr>
                    <th>#</th>
                    <th>Anime title</th>
                    <th>Score</th>
                    <th>Type</th>
                    <th>Merged</th>
                    <th>Watched</th>
                    <th>Synced at</th>
                  </tr>
                </thead>
                <tbody>
                  {anime.map((item, index) => (
                    <tr key={`${item.type}-${item.id}`}>
                      <td className="rank-cell">{index + 1}</td>
                      <td className="title-cell">
                        <div className="title-block">
                          <span className="title-main">{item.display_title}</span>
                          <div className="title-meta">
                            <span className="anime-id">ID #{item.id}</span>
                            <span
                              className={`type-pill type-pill-${item.type}`}
                            >
                              {formatTypeLabel(item.type)}
                            </span>
                          </div>
                        </div>
                      </td>
                      <td data-label="Score" className="numeric-cell">
                        {formatScore(item.avg_score)}
                      </td>
                      <td data-label="Type">
                        <span className={`type-badge type-${item.type}`}>
                          {formatTypeLabel(item.type)}
                        </span>
                      </td>
                      <td data-label="Merged" className="numeric-cell">
                        {item.merged_titles}
                      </td>
                      <td data-label="Watched" className="numeric-cell">
                        {item.watched_episodes_sum}
                      </td>
                      <td data-label="Synced at" className="synced-cell">
                        {formatSyncedAt(item.synced_at)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
      </section>
    </main>
  )
}

export default App
