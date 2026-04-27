import { useEffect, useRef, useState } from 'react'
import {
  authStartUrl,
  fetchAnime,
  fetchCurrentUser,
  fetchPublicAnime,
  fetchPublicStats,
  fetchSyncJob,
  fetchStats,
  logout,
  startPublicSync,
  startSync,
  syncJobEventsUrl,
} from './api'
import AnimeDetailsSection from './components/AnimeDetailsSection'
import AnimeListSection from './components/AnimeListSection'
import PublicSearch from './components/PublicSearch'
import StatsGrid from './components/StatsGrid'
import StatusBlock from './components/StatusBlock'
import UserControls from './components/UserControls'
import UserPage from './components/UserPage'
import useScrollBackground from './useScrollBackground'

const emptyStats = {
  series_count: 0,
  movies_count: 0,
  total_count: 0,
}

function formatSyncProgressMessage(job) {
  if (!job) {
    return ''
  }

  if (job.status === 'completed') {
    return job.message || 'Sync completed.'
  }

  if (job.status === 'failed') {
    return job.error || job.message || 'Sync failed.'
  }

  if (job.total > 0) {
    return `${job.message} (${job.current}/${job.total})`
  }

  return job.message || 'Sync is running...'
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

function readIsUserPageOpen() {
  if (typeof window === 'undefined') {
    return false
  }

  return window.location.hash === '#/user'
}

function openAnimeRoute(animeId) {
  if (typeof window === 'undefined') {
    return
  }

  window.location.hash = `/anime/${animeId}`
}

function clearRoute() {
  if (typeof window === 'undefined') {
    return
  }

  window.history.replaceState(
    null,
    '',
    `${window.location.pathname}${window.location.search}`,
  )
}

function clearAnimeRoute() {
  if (typeof window === 'undefined') {
    return
  }

  if (readSelectedAnimeId() !== null) {
    clearRoute()
  }
}

function openUserRoute() {
  if (typeof window === 'undefined') {
    return
  }

  window.location.hash = '/user'
}

function App() {
  useScrollBackground()

  const listRegionRef = useRef(null)
  const shouldRestoreListFocusRef = useRef(false)
  const syncEventsRef = useRef(null)
  const [currentUser, setCurrentUser] = useState(null)
  const [dashboardUser, setDashboardUser] = useState(null)
  const [publicUsername, setPublicUsername] = useState('')
  const [selectedAnimeId, setSelectedAnimeId] = useState(readSelectedAnimeId)
  const [isUserPageOpen, setIsUserPageOpen] = useState(readIsUserPageOpen)
  const [stats, setStats] = useState(emptyStats)
  const [anime, setAnime] = useState([])
  const [isCheckingSession, setIsCheckingSession] = useState(true)
  const [isLoading, setIsLoading] = useState(false)
  const [isSyncing, setIsSyncing] = useState(false)
  const [isPublicSyncing, setIsPublicSyncing] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [statusMessage, setStatusMessage] = useState('Checking MAL session...')
  const [syncProgress, setSyncProgress] = useState(null)
  const isDetailsOpen = !isUserPageOpen && selectedAnimeId !== null
  const activeUsername = dashboardUser?.username ?? ''
  const activeDashboardMode = dashboardUser?.mode ?? null

  function resetAnimeSelection() {
    clearAnimeRoute()
    setSelectedAnimeId(null)
  }

  function showDashboardRoute() {
    clearRoute()
    setIsUserPageOpen(false)
    setSelectedAnimeId(null)
  }

  function closeSyncEvents() {
    syncEventsRef.current?.close()
    syncEventsRef.current = null
  }

  function clearSyncProgress() {
    closeSyncEvents()
    setSyncProgress(null)
  }

  function setSyncBusy(context, value) {
    if (context.mode === 'public') {
      setIsPublicSyncing(value)
      return
    }

    setIsSyncing(value)
  }

  function finishSyncJob(context, job) {
    closeSyncEvents()
    setSyncProgress(job)
    setStatusMessage(formatSyncProgressMessage(job))
    setSyncBusy(context, false)

    if (job.status === 'completed') {
      if (context.mode === 'public') {
        void loadPublicDashboard(context.username, { preserveProgress: true })
        return
      }

      void loadSessionDashboard(context.user, { preserveProgress: true })
      return
    }

    if (job.status === 'failed') {
      setErrorMessage(job.error || job.message || 'Sync failed.')
    }
  }

  function watchSyncJob(jobId, context) {
    if (!jobId) {
      setSyncBusy(context, false)
      return
    }

    closeSyncEvents()

    const source = new EventSource(syncJobEventsUrl(jobId), {
      withCredentials: true,
    })
    syncEventsRef.current = source

    source.onmessage = (event) => {
      const job = JSON.parse(event.data)
      setSyncProgress(job)
      setStatusMessage(formatSyncProgressMessage(job))

      if (job.status === 'completed' || job.status === 'failed') {
        finishSyncJob(context, job)
      }
    }

    source.onerror = () => {
      source.close()
      if (syncEventsRef.current === source) {
        syncEventsRef.current = null
      }

      void fetchSyncJob(jobId)
        .then((job) => {
          setSyncProgress(job)
          setStatusMessage(formatSyncProgressMessage(job))
          if (job.status === 'completed' || job.status === 'failed') {
            finishSyncJob(context, job)
            return
          }

          setSyncBusy(context, false)
          setErrorMessage('Lost connection to sync progress. Refresh the list in a few seconds.')
        })
        .catch((error) => {
          setSyncBusy(context, false)
          setErrorMessage(error.message)
          setStatusMessage('Lost connection to sync progress.')
        })
    }
  }

  async function loadSessionDashboard(user = currentUser, options = {}) {
    if (!user) {
      setErrorMessage('Sign in with MAL first.')
      return
    }

    if (!options.preserveProgress) {
      clearSyncProgress()
    }
    resetAnimeSelection()
    setDashboardUser({ mode: 'session', username: user.username })
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
      if (options.preserveProgress) {
        setSyncProgress(null)
      }
      setIsLoading(false)
    }
  }

  async function loadPublicDashboard(username, options = {}) {
    const nextUsername = username.trim()
    if (!nextUsername) {
      setErrorMessage('Enter a MAL username.')
      return
    }

    if (!options.preserveProgress) {
      clearSyncProgress()
    }
    resetAnimeSelection()
    setDashboardUser({ mode: 'public', username: nextUsername })
    setIsLoading(true)
    setErrorMessage('')
    setStatusMessage(`Loading public list for ${nextUsername}...`)

    try {
      const [nextStats, nextAnime] = await Promise.all([
        fetchPublicStats(nextUsername),
        fetchPublicAnime(nextUsername),
      ])

      setStats(nextStats)
      setAnime(nextAnime)
      setStatusMessage(
        nextAnime.length > 0
          ? `Loaded ${nextAnime.length} grouped anime entries for ${nextUsername}.`
          : `${nextUsername} has no synced public anime yet.`,
      )
    } catch (error) {
      setStats(emptyStats)
      setAnime([])
      setErrorMessage(error.message)
      setStatusMessage(`Could not load public list for ${nextUsername}.`)
    } finally {
      if (options.preserveProgress) {
        setSyncProgress(null)
      }
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
        void loadSessionDashboard(response.user)
      } catch {
        setCurrentUser(null)
        setDashboardUser(null)
        setStats(emptyStats)
        setAnime([])
        setStatusMessage('Search a public MAL username or sign in with MAL.')
      } finally {
        setIsCheckingSession(false)
      }
    }

    void loadSession()
  }, [])

  useEffect(() => {
    return () => {
      closeSyncEvents()
    }
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') {
      return undefined
    }

    function handleHashChange() {
      setIsUserPageOpen(readIsUserPageOpen())
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
      clearSyncProgress()
      showDashboardRoute()
      setCurrentUser(null)
      setDashboardUser(null)
      setStats(emptyStats)
      setAnime([])
      setStatusMessage('Signed out. Search a public MAL username or sign in with MAL.')
    }
  }

  async function handleSync() {
    if (!currentUser) {
      setErrorMessage('Sign in with MAL first.')
      return
    }

    try {
      clearSyncProgress()
      showDashboardRoute()
      setDashboardUser({ mode: 'session', username: currentUser.username })
      setStats(emptyStats)
      setAnime([])
      setIsSyncing(true)
      setErrorMessage('')

      const response = await startSync()
      setStatusMessage(response.message)
      watchSyncJob(response.job_id, {
        mode: 'session',
        username: currentUser.username,
        user: currentUser,
      })
    } catch (error) {
      setErrorMessage(error.message)
      setIsSyncing(false)
    }
  }

  async function handlePublicSync(username) {
    const nextUsername = username.trim()
    if (!nextUsername) {
      setErrorMessage('Enter a MAL username.')
      return
    }

    try {
      clearSyncProgress()
      resetAnimeSelection()
      setDashboardUser({ mode: 'public', username: nextUsername })
      setStats(emptyStats)
      setAnime([])
      setIsPublicSyncing(true)
      setErrorMessage('')

      const response = await startPublicSync(nextUsername)
      setStatusMessage(response.message)
      watchSyncJob(response.job_id, {
        mode: 'public',
        username: nextUsername,
      })
    } catch (error) {
      setErrorMessage(error.message)
      setStatusMessage(`Could not start public sync for ${nextUsername}.`)
      setIsPublicSyncing(false)
    }
  }

  async function handleSessionRefresh() {
    showDashboardRoute()
    await loadSessionDashboard()
  }

  function handleOpenUserPage() {
    if (!currentUser) {
      setErrorMessage('Sign in with MAL first.')
      return
    }

    resetAnimeSelection()
    openUserRoute()
    setIsUserPageOpen(true)
  }

  function handleUserPageBack() {
    showDashboardRoute()
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
      <UserControls
        currentUser={currentUser}
        onLogin={handleLogin}
        onLogout={handleLogout}
        onOpenUserPage={handleOpenUserPage}
        onSync={handleSync}
        onRefresh={handleSessionRefresh}
        isCheckingSession={isCheckingSession}
        isLoading={isLoading && activeDashboardMode === 'session'}
        isSyncing={isSyncing}
        isUserPageOpen={isUserPageOpen}
      />

      {isUserPageOpen ? (
        <UserPage
          currentUser={currentUser}
          isCheckingSession={isCheckingSession}
          onBack={handleUserPageBack}
        />
      ) : (
      <section className="dashboard">
        <header className="hero-card">
          <p className="eyebrow">MAL Dashboard</p>
          <h1>Explore a MyAnimeList profile</h1>
          <p className="lead">
            Search by MAL username for public lists, or use your signed-in
            account from the top bar.
          </p>
        </header>

        <section className="panel control-panel">
          <PublicSearch
            username={publicUsername}
            onUsernameChange={setPublicUsername}
            onSearch={loadPublicDashboard}
            onSync={handlePublicSync}
            isLoading={isLoading && activeDashboardMode === 'public'}
            isSyncing={isPublicSyncing}
          />

          {/* Feedback for the current request state */}
          <StatusBlock
            statusMessage={statusMessage}
            errorMessage={errorMessage}
            mode={activeDashboardMode}
            progress={syncProgress}
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
      )}
    </main>
  )
}

export default App
