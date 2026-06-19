import { useCallback, useEffect, useRef, useState } from 'react'
import useHashRoute from './app/useHashRoute'
import useSeasonRoute from './app/useSeasonRoute'
import AnimeDetailsSection from './components/AnimeDetailsSection'
import AnimeListSection from './components/AnimeListSection'
import Footer from './components/Footer'
import PublicSearch from './components/PublicSearch'
import SeasonPage from './components/SeasonPage'
import StatsGrid from './components/StatsGrid'
import StatusBlock from './components/StatusBlock'
import UserControls from './components/UserControls'
import UserPage from './components/UserPage'
import useDashboardController from './features/dashboard/useDashboardController'
import useListEdit from './features/listEdit/useListEdit'
import useSeasonBrowser from './features/seasonBrowser/useSeasonBrowser'
import useSyncJob from './features/syncJob/useSyncJob'
import {
  authStartUrl,
  fetchAnime,
  fetchCurrentUser,
  fetchStats,
  logout,
  startPublicSync,
  startSync,
} from './shared/api/api'
import { parseMalUsername } from './shared/security/inputValidation'
import { findFranchiseGroupIdByMemberId } from './entities/anime/animeSelectors'
import useScrollBackground from './app/useScrollBackground'

const PUBLIC_SEARCH_DEBOUNCE_MS = 400
const PUBLIC_SYNC_COOLDOWN_MS = 15000
const LIST_EDIT_REFRESH_DEBOUNCE_MS = 1500

function publicSyncKey(username) {
  return username.toLowerCase()
}

function isSameMalUsername(leftUsername, rightUsername) {
  return Boolean(leftUsername && rightUsername)
    && publicSyncKey(leftUsername) === publicSyncKey(rightUsername)
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
  const seasonRoute = useSeasonRoute()
  const seasonBrowser = useSeasonBrowser(seasonRoute.season)
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
  const activeDashboardMode = dashboard.activeDashboardMode
  const activeDashboard =
    activeDashboardMode === 'session'
      ? dashboard.sessionDashboard
      : dashboard.publicDashboard
  const activeUsername = activeDashboard.user?.username ?? ''
  const activeErrorMessage = dashboard.errorMessage || activeDashboard.error

  const listEditRefreshTimerRef = useRef(null)
  const currentUserRef = useRef(null)
  currentUserRef.current = currentUser

  // Group aggregates (status counts, episode sums) are computed by the
  // backend, so quietly refetch them shortly after the last list edit.
  const scheduleSessionDashboardRefresh = useCallback(() => {
    if (listEditRefreshTimerRef.current) {
      window.clearTimeout(listEditRefreshTimerRef.current)
    }

    listEditRefreshTimerRef.current = window.setTimeout(async () => {
      listEditRefreshTimerRef.current = null
      const user = currentUserRef.current
      if (!user) {
        return
      }

      try {
        const [stats, anime] = await Promise.all([fetchStats(), fetchAnime()])
        dashboard.hydrateSessionDashboard(user, { stats, anime })
      } catch {
        // Keep the optimistic state; the next manual refresh will reconcile.
      }
    }, LIST_EDIT_REFRESH_DEBOUNCE_MS)
  }, [dashboard.hydrateSessionDashboard])

  const handleListEntryUpdated = useCallback((entry) => {
    dashboard.applySessionListEntryUpdate(entry)
    scheduleSessionDashboardRefresh()
  }, [dashboard.applySessionListEntryUpdate, scheduleSessionDashboardRefresh])

  const handleListEntryRemoved = useCallback((animeId) => {
    dashboard.applySessionListEntryRemoval(animeId)
    scheduleSessionDashboardRefresh()
  }, [dashboard.applySessionListEntryRemoval, scheduleSessionDashboardRefresh])

  const listEdit = useListEdit({
    onEntryUpdated: handleListEntryUpdated,
    onEntryRemoved: handleListEntryRemoved,
    onErrorMessage: dashboard.setErrorMessage,
  })

  useEffect(() => {
    return () => {
      if (listEditRefreshTimerRef.current) {
        window.clearTimeout(listEditRefreshTimerRef.current)
      }
    }
  }, [])

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
    await dashboard.loadSessionDashboard(user, {
      activate: options.activate,
    })

    if (options.preserveProgress) {
      syncJob.clearFinishedProgress()
    }
  }

  async function loadPublicDashboard(username, options = {}) {
    if (!options.preserveProgress) {
      syncJob.clearSyncProgress()
    }

    route.clearAnimeRoute()
    const snapshot = await dashboard.loadPublicDashboard(username, {
      activate: options.activate,
    })

    if (
      snapshot
      && currentUser
      && isSameMalUsername(snapshot.username, currentUser.username)
    ) {
      dashboard.hydrateSessionDashboard(currentUser, snapshot)
    }

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
      dashboard.prepareSessionDashboard(currentUser)
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
      dashboard.preparePublicDashboard(nextUsername)
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

  function handleOpenUserPage() {
    if (!currentUser) {
      dashboard.setErrorMessage('Sign in with MAL first.')
      return
    }

    route.clearAnimeRoute()
    if (
      !dashboard.sessionDashboard.user
      || !isSameMalUsername(dashboard.sessionDashboard.user.username, currentUser.username)
    ) {
      void loadSessionDashboard(currentUser, {
        activate: false,
        preserveProgress: true,
      })
    }
    route.openUserRoute()
  }

  function handleUserPageBack() {
    route.showDashboardRoute()
  }

  function handleOpenSeasons() {
    // Reset the hash-based route so no stale user/anime view lingers behind the
    // seasonal page once it is closed.
    route.showDashboardRoute()
    seasonRoute.openSeason()
  }

  function handleSeasonAnimeSelect(animeId) {
    if (!animeId) {
      return
    }

    // A season card carries an individual anime id; resolve it to the franchise
    // group representative so the dashboard's franchise renderer can find it.
    const groupId =
      findFranchiseGroupIdByMemberId(activeDashboard.anime, animeId) ?? animeId

    seasonRoute.closeSeason()
    route.openAnimeRoute(groupId)
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
        onOpenSeasons={handleOpenSeasons}
        onReload={handleSync}
        isCheckingSession={isCheckingSession}
        isReloading={syncJob.isSessionSyncing || dashboard.sessionDashboard.isLoading}
        isUserPageOpen={route.isUserPageOpen}
        isSeasonsOpen={seasonRoute.isSeasonOpen}
      />

      {seasonRoute.isSeasonOpen ? (
        <SeasonPage
          season={seasonRoute.season}
          anime={seasonBrowser.anime}
          isLoading={seasonBrowser.isLoading}
          error={seasonBrowser.error}
          onNavigate={seasonRoute.openSeason}
          onBack={seasonRoute.closeSeason}
          onSelectAnime={handleSeasonAnimeSelect}
        />
      ) : route.isUserPageOpen ? (
        <UserPage
          currentUser={currentUser}
          stats={dashboard.sessionDashboard.stats}
          anime={dashboard.sessionDashboard.anime}
          isLoading={dashboard.sessionDashboard.isLoading || isCheckingSession}
          isCheckingSession={isCheckingSession}
          onBack={handleUserPageBack}
        />
      ) : (
        <section className="dashboard">
          <header className="hero-card">
            <p className="eyebrow">anikoro Dashboard</p>
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
                || dashboard.publicDashboard.isLoading
              }
              isSyncing={syncJob.isPublicSyncing}
              syncCooldownSeconds={publicSyncCooldownSeconds}
            />

            <StatusBlock
              statusMessage={dashboard.statusMessage}
              errorMessage={activeErrorMessage}
              mode={activeDashboardMode}
              progress={syncJob.syncProgress}
            />
          </section>

          <StatsGrid
            stats={activeDashboard.stats}
            isLoading={activeDashboard.isLoading || isCheckingSession}
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
              anime={activeDashboard.anime}
              isLoading={activeDashboard.isLoading || isCheckingSession}
              onSelectAnime={handleAnimeSelect}
            />
          </div>

          {route.isDetailsOpen ? (
            <AnimeDetailsSection
              activeUsername={activeUsername}
              anime={activeDashboard.anime}
              selectedAnimeId={route.selectedAnimeId}
              isLoading={activeDashboard.isLoading || isCheckingSession}
              onBack={handleAnimeBack}
              canEditList={activeDashboardMode === 'session' && Boolean(currentUser)}
              pendingAnimeIds={listEdit.pendingAnimeIds}
              onUpdateListEntry={listEdit.updateListEntry}
              onRemoveListEntry={listEdit.removeListEntry}
            />
          ) : null}
        </section>
      )}

      <Footer />
    </main>
  )
}

export default App
