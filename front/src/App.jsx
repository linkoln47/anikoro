import { useCallback, useEffect, useRef, useState } from 'react'
import useHashRoute from './app/useHashRoute'
import usePathRoute from './app/usePathRoute'
import AllAnimePage from './components/AllAnimePage'
import AnimeDetailsSection from './components/AnimeDetailsSection'
import AnimeListSection from './components/AnimeListSection'
import Footer from './components/Footer'
import PublicSearch from './components/PublicSearch'
import SeasonPage from './components/SeasonPage'
import AuthPanel from './components/AuthPanel'
import StatsGrid from './components/StatsGrid'
import StatusBlock from './components/StatusBlock'
import UserControls from './components/UserControls'
import UserPage from './components/UserPage'
import useDashboardController from './features/dashboard/useDashboardController'
import useFranchise from './features/franchise/useFranchise'
import useFranchises from './features/franchise/useFranchises'
import useListEdit from './features/listEdit/useListEdit'
import useSeasonBrowser from './features/seasonBrowser/useSeasonBrowser'
import useSyncJob from './features/syncJob/useSyncJob'
import {
  authStartUrl,
  disconnectMal,
  fetchAnime,
  fetchCurrentUser,
  fetchStats,
  login,
  logout,
  register,
  startSync,
} from './shared/api/api'
import { parseAccountUsername } from './shared/security/inputValidation'
import useScrollBackground from './app/useScrollBackground'

const PUBLIC_SEARCH_DEBOUNCE_MS = 400
const LIST_EDIT_REFRESH_DEBOUNCE_MS = 1500

function usernameKey(username) {
  return username.toLowerCase()
}

function isSameUsername(leftUsername, rightUsername) {
  return Boolean(leftUsername && rightUsername)
    && usernameKey(leftUsername) === usernameKey(rightUsername)
}

function App() {
  useScrollBackground()

  const listRegionRef = useRef(null)
  const publicSearchDebounceRef = useRef(null)
  const shouldRestoreListFocusRef = useRef(false)
  const [currentUser, setCurrentUser] = useState(null)
  const [searchedUsername, setSearchedUsername] = useState('')
  const [isPublicSearchQueued, setIsPublicSearchQueued] = useState(false)
  const [isCheckingSession, setIsCheckingSession] = useState(true)
  const [authPanelMode, setAuthPanelMode] = useState(null)
  const [authError, setAuthError] = useState('')
  const [isAuthSubmitting, setIsAuthSubmitting] = useState(false)
  const [isMalDisconnecting, setIsMalDisconnecting] = useState(false)
  const route = useHashRoute()
  const pathRoute = usePathRoute()
  const seasonBrowser = useSeasonBrowser(pathRoute.season)
  const seasonFranchise = useFranchise(
    pathRoute.isFranchiseOpen ? pathRoute.franchiseId : null,
  )
  const allFranchises = useFranchises(pathRoute.isFranchisesOpen)
  const dashboard = useDashboardController()

  const syncJob = useSyncJob({
    onErrorMessage: dashboard.setErrorMessage,
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
      && isSameUsername(snapshot.username, currentUser.username)
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
      nextUsername = parseAccountUsername(username)
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
        dashboard.setStatusMessage('Search an anikoro username or sign in.')
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
    if (route.isDetailsOpen) {
      return
    }

    if (!shouldRestoreListFocusRef.current) {
      return
    }

    listRegionRef.current?.focus()
    shouldRestoreListFocusRef.current = false
  }, [route.isDetailsOpen])

  function handleOpenSignIn() {
    setAuthError('')
    setAuthPanelMode('login')
  }

  function handleOpenRegister() {
    setAuthError('')
    setAuthPanelMode('register')
  }

  function handleCloseAuthPanel() {
    if (isAuthSubmitting) {
      return
    }
    setAuthError('')
    setAuthPanelMode(null)
  }

  function handleConnectMal() {
    if (!currentUser || typeof window === 'undefined') {
      return
    }

    window.location.assign(authStartUrl())
  }

  async function handleDisconnectMal() {
    if (isMalDisconnecting) {
      return
    }

    setIsMalDisconnecting(true)
    dashboard.setErrorMessage('')

    try {
      // Disconnect drops the MAL link and token but keeps the synced snapshot,
      // so the dashboard data stays intact.
      const response = await disconnectMal()
      if (response.user) {
        setCurrentUser(response.user)
      }
      dashboard.setStatusMessage('MAL account disconnected. Your synced data is kept.')
    } catch (error) {
      dashboard.setErrorMessage(error.message)
    } finally {
      setIsMalDisconnecting(false)
    }
  }

  async function handleAuthSubmit({ mode, email, username, password }) {
    setIsAuthSubmitting(true)
    setAuthError('')

    try {
      const response =
        mode === 'register'
          ? await register({ email, username, password })
          : await login({ email, password })

      if (!response.authenticated || !response.user) {
        throw new Error('Authentication failed. Please try again.')
      }

      setCurrentUser(response.user)
      setAuthPanelMode(null)
      dashboard.setErrorMessage('')
      dashboard.setStatusMessage(`Signed in as ${response.user.username}.`)
      route.showDashboardRoute()
      void loadSessionDashboard(response.user)
    } catch (error) {
      setAuthError(error.message)
    } finally {
      setIsAuthSubmitting(false)
    }
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
      syncJob.clearSyncProgress()
      route.showDashboardRoute()
      setCurrentUser(null)
      dashboard.clearDashboard()
      dashboard.setStatusMessage('Signed out. Search an anikoro username or sign in.')
    }
  }

  async function handleSync() {
    if (!currentUser) {
      dashboard.setErrorMessage('Connect your MAL account first.')
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

  function handleOpenUserPage() {
    if (!currentUser) {
      dashboard.setErrorMessage('Sign in first.')
      return
    }

    route.clearAnimeRoute()
    if (
      !dashboard.sessionDashboard.user
      || !isSameUsername(dashboard.sessionDashboard.user.username, currentUser.username)
    ) {
      void loadSessionDashboard(currentUser, {
        activate: false,
        preserveProgress: true,
      })
    }
    route.openUserRoute()
  }

  function handleOpenDashboard() {
    // Return to the root dashboard from any browse view (season grid, franchise
    // page, user page, or an open anime detail).
    pathRoute.resetToDashboard()
    route.showDashboardRoute()
  }

  function handleOpenSeasons() {
    // Reset the hash-based route so no stale user/anime view lingers behind the
    // seasonal page once it is closed.
    route.showDashboardRoute()
    pathRoute.openSeason()
  }

  function handleOpenAllAnime() {
    // Clear the hash route for the same reason as the seasonal page before
    // pushing the catalog-wide franchise grid.
    route.showDashboardRoute()
    pathRoute.openFranchises()
  }

  function handleSeasonAnimeSelect(animeId) {
    if (!animeId) {
      return
    }

    // Navigate to the dedicated franchise page (/franchise/{id}). The backend
    // resolves the franchise group for any catalog anime id, so the page opens
    // with or without a session.
    pathRoute.openFranchise(animeId)
  }

  // The seasonal franchise overlay reuses the dashboard list-edit machinery, so
  // a signed-in user edits the same entity here. Refetch the franchise after a
  // successful change so the overlay reflects the updated marks.
  async function handleSeasonFranchiseUpdate(animeId, patch) {
    const entry = await listEdit.updateListEntry(animeId, patch)
    if (entry) {
      seasonFranchise.reload()
    }
    return entry
  }

  async function handleSeasonFranchiseRemove(animeId) {
    const result = await listEdit.removeListEntry(animeId)
    if (result) {
      seasonFranchise.reload()
    }
    return result
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
        onOpenSignIn={handleOpenSignIn}
        onOpenRegister={handleOpenRegister}
        onLogout={handleLogout}
        onOpenDashboard={handleOpenDashboard}
        onOpenUserPage={handleOpenUserPage}
        onOpenSeasons={handleOpenSeasons}
        onOpenAllAnime={handleOpenAllAnime}
        onReload={handleSync}
        isCheckingSession={isCheckingSession}
        isReloading={syncJob.isSessionSyncing || dashboard.sessionDashboard.isLoading}
        isDashboardActive={
          !pathRoute.isSeasonOpen
          && !pathRoute.isFranchiseOpen
          && !pathRoute.isFranchisesOpen
          && !route.isUserPageOpen
        }
        isUserPageOpen={route.isUserPageOpen}
        isSeasonsOpen={pathRoute.isSeasonOpen || pathRoute.isFranchiseOpen}
        isAllAnimeOpen={pathRoute.isFranchisesOpen}
      />

      {authPanelMode ? (
        <AuthPanel
          initialMode={authPanelMode}
          onSubmit={handleAuthSubmit}
          onCancel={handleCloseAuthPanel}
          isSubmitting={isAuthSubmitting}
          serverError={authError}
        />
      ) : null}

      {pathRoute.isFranchiseOpen ? (
        <AnimeDetailsSection
          activeUsername={seasonFranchise.franchise?.display_title || 'Franchise'}
          anime={seasonFranchise.franchise ? [seasonFranchise.franchise] : []}
          selectedAnimeId={seasonFranchise.franchise?.id ?? pathRoute.franchiseId}
          isLoading={seasonFranchise.isLoading}
          onBack={pathRoute.closeFranchise}
          backLabel="Back to seasons"
          canEditList={Boolean(currentUser)}
          pendingAnimeIds={listEdit.pendingAnimeIds}
          onUpdateListEntry={handleSeasonFranchiseUpdate}
          onRemoveListEntry={handleSeasonFranchiseRemove}
        />
      ) : pathRoute.isSeasonOpen ? (
        <SeasonPage
          season={pathRoute.season}
          anime={seasonBrowser.anime}
          isLoading={seasonBrowser.isLoading}
          error={seasonBrowser.error}
          onNavigate={pathRoute.openSeason}
          onSelectAnime={handleSeasonAnimeSelect}
        />
      ) : pathRoute.isFranchisesOpen ? (
        <AllAnimePage onSelectFranchise={handleSeasonAnimeSelect} />
      ) : route.isUserPageOpen && currentUser ? (
        <UserPage
          currentUser={currentUser}
          stats={dashboard.sessionDashboard.stats}
          anime={dashboard.sessionDashboard.anime}
          isLoading={dashboard.sessionDashboard.isLoading || isCheckingSession}
          isCheckingSession={isCheckingSession}
          onConnectMal={handleConnectMal}
          onDisconnectMal={handleDisconnectMal}
          isMalBusy={isMalDisconnecting}
        />
      ) : (
        <section className="dashboard">
          <header className="hero-card">
            <p className="eyebrow">anikoro Dashboard</p>
            <h1>Explore an anikoro profile</h1>
            <p className="lead">
              Search by native account username, or use your signed-in
              account from the top bar.
            </p>
          </header>

          <section className="panel control-panel">
            <PublicSearch
              username={searchedUsername}
              onUsernameChange={setSearchedUsername}
              onSearch={handlePublicSearch}
              isLoading={
                isPublicSearchQueued
                || dashboard.publicDashboard.isLoading
              }
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
