import { useCallback, useEffect, useRef, useState } from 'react'
import useHashRoute from './app/useHashRoute'
import AnimeDetailsSection from './components/AnimeDetailsSection'
import AnimeListSection from './components/AnimeListSection'
import PublicSearch from './components/PublicSearch'
import StatsGrid from './components/StatsGrid'
import StatusBlock from './components/StatusBlock'
import UserControls from './components/UserControls'
import UserPage from './components/UserPage'
import useDashboardController from './features/dashboard/useDashboardController'
import useSyncJob from './features/syncJob/useSyncJob'
import {
  authStartUrl,
  fetchCurrentUser,
  logout,
  startPublicSync,
  startSync,
} from './shared/api/malApi'
import { parseMalUsername } from './shared/security/inputValidation'
import useScrollBackground from './app/useScrollBackground'

const PUBLIC_SEARCH_DEBOUNCE_MS = 400
const PUBLIC_SYNC_COOLDOWN_MS = 15000

function publicSyncKey(username) {
  return username.toLowerCase()
}

function App() {
  useScrollBackground()

  const listRegionRef = useRef(null)
  const publicSearchDebounceRef = useRef(null)
  const publicSyncCooldownUntilRef = useRef(0)
  const publicSyncInFlightRef = useRef(new Set())
  const shouldRestoreListFocusRef = useRef(false)
  const [currentUser, setCurrentUser] = useState(null)
  const [publicUsername, setPublicUsername] = useState('')
  const [isPublicSearchQueued, setIsPublicSearchQueued] = useState(false)
  const [publicSyncCooldownUntil, setPublicSyncCooldownUntil] = useState(0)
  const [publicSyncCooldownNow, setPublicSyncCooldownNow] = useState(() => Date.now())
  const [isCheckingSession, setIsCheckingSession] = useState(true)
  const route = useHashRoute()
  const dashboard = useDashboardController()
  const publicSyncCooldownRemainingMs = Math.max(
    0,
    publicSyncCooldownUntil - publicSyncCooldownNow,
  )
  const publicSyncCooldownSeconds = Math.ceil(publicSyncCooldownRemainingMs / 1000)
  const handlePublicSyncFinished = useCallback((context) => {
    publicSyncInFlightRef.current.delete(publicSyncKey(context.username))
  }, [])
  const syncJob = useSyncJob({
    onErrorMessage: dashboard.setErrorMessage,
    onPublicCompleted: (context) => {
      void loadPublicDashboard(context.username, { preserveProgress: true })
    },
    onPublicFinished: handlePublicSyncFinished,
    onSessionCompleted: (context) => {
      void loadSessionDashboard(context.user, { preserveProgress: true })
    },
    onStatusMessage: dashboard.setStatusMessage,
  })
  const activeUsername = dashboard.dashboardUser?.username ?? ''
  const activeDashboardMode = dashboard.dashboardUser?.mode ?? null

  const cancelQueuedPublicSearch = useCallback(() => {
    if (publicSearchDebounceRef.current) {
      window.clearTimeout(publicSearchDebounceRef.current)
      publicSearchDebounceRef.current = null
    }

    setIsPublicSearchQueued(false)
  }, [])

  function startPublicSyncCooldown() {
    const now = Date.now()
    const cooldownUntil = now + PUBLIC_SYNC_COOLDOWN_MS

    publicSyncCooldownUntilRef.current = cooldownUntil
    setPublicSyncCooldownNow(now)
    setPublicSyncCooldownUntil(cooldownUntil)
  }

  async function loadSessionDashboard(user = currentUser, options = {}) {
    if (!options.preserveProgress) {
      syncJob.clearSyncProgress()
    }

    route.clearAnimeRoute()
    await dashboard.loadSessionDashboard(user)

    if (options.preserveProgress) {
      syncJob.clearFinishedProgress()
    }
  }

  async function loadPublicDashboard(username, options = {}) {
    if (!options.preserveProgress) {
      syncJob.clearSyncProgress()
    }

    route.clearAnimeRoute()
    await dashboard.loadPublicDashboard(username)

    if (options.preserveProgress) {
      syncJob.clearFinishedProgress()
    }
  }

  function handlePublicSearch(username) {
    let nextUsername
    try {
      nextUsername = parseMalUsername(username)
    } catch (error) {
      dashboard.setErrorMessage(error.message)
      return
    }

    cancelQueuedPublicSearch()
    dashboard.cancelPublicDashboardLoad()
    dashboard.setErrorMessage('')
    setIsPublicSearchQueued(true)

    publicSearchDebounceRef.current = window.setTimeout(() => {
      publicSearchDebounceRef.current = null
      setIsPublicSearchQueued(false)
      void loadPublicDashboard(nextUsername)
    }, PUBLIC_SEARCH_DEBOUNCE_MS)
  }

  useEffect(() => {
    async function loadSession() {
      setIsCheckingSession(true)
      dashboard.setErrorMessage('')

      try {
        const response = await fetchCurrentUser()
        if (!response.authenticated || !response.user) {
          throw new Error('No active MAL session')
        }

        setCurrentUser(response.user)
        dashboard.setStatusMessage(`Signed in as ${response.user.username}. Loading dashboard...`)
        void loadSessionDashboard(response.user)
      } catch {
        setCurrentUser(null)
        dashboard.clearDashboard()
        dashboard.setStatusMessage('Search a public MAL username or sign in with MAL.')
      } finally {
        setIsCheckingSession(false)
      }
    }

    void loadSession()
  }, [])

  useEffect(() => {
    return () => {
      cancelQueuedPublicSearch()
    }
  }, [cancelQueuedPublicSearch])

  useEffect(() => {
    if (publicSyncCooldownRemainingMs <= 0) {
      return undefined
    }

    const timeout = window.setTimeout(() => {
      setPublicSyncCooldownNow(Date.now())
    }, Math.min(publicSyncCooldownRemainingMs, 1000))

    return () => {
      window.clearTimeout(timeout)
    }
  }, [publicSyncCooldownRemainingMs])

  useEffect(() => {
    if (route.isDetailsOpen) {
      return
    }

    if (!shouldRestoreListFocusRef.current) {
      return
    }

    listRegionRef.current?.focus()
    shouldRestoreListFocusRef.current = false
  }, [route.isDetailsOpen])

  function handleLogin() {
    if (typeof window === 'undefined') {
      return
    }

    window.location.assign(authStartUrl())
  }

  async function handleLogout() {
    try {
      dashboard.setErrorMessage('')
      await logout()
    } catch (error) {
      dashboard.setErrorMessage(error.message)
    } finally {
      cancelQueuedPublicSearch()
      dashboard.cancelPublicDashboardLoad()
      publicSyncInFlightRef.current.clear()
      syncJob.clearSyncProgress()
      route.showDashboardRoute()
      setCurrentUser(null)
      dashboard.clearDashboard()
      dashboard.setStatusMessage('Signed out. Search a public MAL username or sign in with MAL.')
    }
  }

  async function handleSync() {
    if (!currentUser) {
      dashboard.setErrorMessage('Sign in with MAL first.')
      return
    }

    const context = {
      mode: 'session',
      username: currentUser.username,
      user: currentUser,
    }

    try {
      cancelQueuedPublicSearch()
      dashboard.cancelPublicDashboardLoad()
      publicSyncInFlightRef.current.clear()
      syncJob.clearSyncProgress()
      route.showDashboardRoute()
      dashboard.prepareDashboard({ mode: 'session', username: currentUser.username })
      syncJob.beginSync(context)
      dashboard.setErrorMessage('')

      const response = await startSync()
      dashboard.setStatusMessage(response.message)
      syncJob.watchSyncJob(response.job_id, context)
    } catch (error) {
      dashboard.setErrorMessage(error.message)
      syncJob.endSync(context)
    }
  }

  async function handlePublicSync(username) {
    let nextUsername
    try {
      nextUsername = parseMalUsername(username)
    } catch (error) {
      dashboard.setErrorMessage(error.message)
      return
    }

    const cooldownRemainingMs = Math.max(0, publicSyncCooldownUntilRef.current - Date.now())
    if (cooldownRemainingMs > 0) {
      const cooldownSeconds = Math.ceil(cooldownRemainingMs / 1000)
      setPublicSyncCooldownNow(Date.now())
      dashboard.setErrorMessage(`Wait ${cooldownSeconds} seconds before starting another public sync.`)
      return
    }

    const usernameKey = publicSyncKey(nextUsername)
    if (publicSyncInFlightRef.current.has(usernameKey)) {
      dashboard.setErrorMessage(`${nextUsername} is already syncing.`)
      return
    }

    if (syncJob.activeContext?.mode === 'public') {
      dashboard.setErrorMessage('A public sync is already running.')
      return
    }

    const context = {
      mode: 'public',
      username: nextUsername,
    }

    publicSyncInFlightRef.current.add(usernameKey)
    startPublicSyncCooldown()

    try {
      cancelQueuedPublicSearch()
      syncJob.clearSyncProgress()
      route.clearAnimeRoute()
      dashboard.prepareDashboard({ mode: 'public', username: nextUsername })
      syncJob.beginSync(context)
      dashboard.setErrorMessage('')

      const response = await startPublicSync(nextUsername)
      dashboard.setStatusMessage(response.message)
      syncJob.watchSyncJob(response.job_id, context)
    } catch (error) {
      publicSyncInFlightRef.current.delete(usernameKey)
      dashboard.setErrorMessage(error.message)
      dashboard.setStatusMessage(`Could not start public sync for ${nextUsername}.`)
      syncJob.endSync(context)
    }
  }

  async function handleSessionRefresh() {
    route.showDashboardRoute()
    await loadSessionDashboard()
  }

  function handleOpenUserPage() {
    if (!currentUser) {
      dashboard.setErrorMessage('Sign in with MAL first.')
      return
    }

    route.clearAnimeRoute()
    route.openUserRoute()
  }

  function handleUserPageBack() {
    route.showDashboardRoute()
  }

  function handleAnimeSelect(animeId) {
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur()
    }

    route.openAnimeRoute(animeId)
  }

  function handleAnimeBack() {
    shouldRestoreListFocusRef.current = true
    route.clearAnimeRoute()
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
        isLoading={dashboard.isLoading && activeDashboardMode === 'session'}
        isSyncing={syncJob.isSessionSyncing}
        isUserPageOpen={route.isUserPageOpen}
      />

      {route.isUserPageOpen ? (
        <UserPage
          currentUser={currentUser}
          stats={dashboard.stats}
          anime={dashboard.anime}
          isLoading={dashboard.isLoading || isCheckingSession}
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
              onSearch={handlePublicSearch}
              onSync={handlePublicSync}
              isLoading={
                isPublicSearchQueued
                || (dashboard.isLoading && activeDashboardMode === 'public')
              }
              isSyncing={syncJob.isPublicSyncing}
              syncCooldownSeconds={publicSyncCooldownSeconds}
            />

            <StatusBlock
              statusMessage={dashboard.statusMessage}
              errorMessage={dashboard.errorMessage}
              mode={activeDashboardMode}
              progress={syncJob.syncProgress}
            />
          </section>

          <StatsGrid
            stats={dashboard.stats}
            isLoading={dashboard.isLoading || isCheckingSession}
          />

          <div
            ref={listRegionRef}
            className="list-region-shell"
            tabIndex={-1}
            hidden={route.isDetailsOpen}
            inert={route.isDetailsOpen ? '' : undefined}
          >
            <AnimeListSection
              activeUsername={activeUsername}
              anime={dashboard.anime}
              isLoading={dashboard.isLoading || isCheckingSession}
              onSelectAnime={handleAnimeSelect}
            />
          </div>

          {route.isDetailsOpen ? (
            <AnimeDetailsSection
              activeUsername={activeUsername}
              anime={dashboard.anime}
              selectedAnimeId={route.selectedAnimeId}
              isLoading={dashboard.isLoading || isCheckingSession}
              onBack={handleAnimeBack}
            />
          ) : null}
        </section>
      )}
    </main>
  )
}

export default App
