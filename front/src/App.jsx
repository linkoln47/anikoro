import { useEffect, useRef, useState } from 'react'
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
import useScrollBackground from './app/useScrollBackground'

function App() {
  useScrollBackground()

  const listRegionRef = useRef(null)
  const shouldRestoreListFocusRef = useRef(false)
  const [currentUser, setCurrentUser] = useState(null)
  const [publicUsername, setPublicUsername] = useState('')
  const [isCheckingSession, setIsCheckingSession] = useState(true)
  const route = useHashRoute()
  const dashboard = useDashboardController()
  const syncJob = useSyncJob({
    onErrorMessage: dashboard.setErrorMessage,
    onPublicCompleted: (context) => {
      void loadPublicDashboard(context.username, { preserveProgress: true })
    },
    onSessionCompleted: (context) => {
      void loadSessionDashboard(context.user, { preserveProgress: true })
    },
    onStatusMessage: dashboard.setStatusMessage,
  })
  const activeUsername = dashboard.dashboardUser?.username ?? ''
  const activeDashboardMode = dashboard.dashboardUser?.mode ?? null

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
    const nextUsername = username.trim()
    if (!nextUsername) {
      dashboard.setErrorMessage('Enter a MAL username.')
      return
    }

    const context = {
      mode: 'public',
      username: nextUsername,
    }

    try {
      syncJob.clearSyncProgress()
      route.clearAnimeRoute()
      dashboard.prepareDashboard({ mode: 'public', username: nextUsername })
      syncJob.beginSync(context)
      dashboard.setErrorMessage('')

      const response = await startPublicSync(nextUsername)
      dashboard.setStatusMessage(response.message)
      syncJob.watchSyncJob(response.job_id, context)
    } catch (error) {
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
              onSearch={loadPublicDashboard}
              onSync={handlePublicSync}
              isLoading={dashboard.isLoading && activeDashboardMode === 'public'}
              isSyncing={syncJob.isPublicSyncing}
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
