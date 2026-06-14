function UserControls({
  currentUser,
  onLogin,
  onLogout,
  onOpenUserPage,
  onSync,
  onRefresh,
  isCheckingSession,
  isLoading,
  isSyncing,
  isUserPageOpen,
}) {
  const isSignedIn = Boolean(currentUser)

  return (
    <header className="auth-strip">
      <div className="auth-strip-inner">
        <div className="auth-strip-title">
          <span className="field-label">anikoro Dashboard</span>
        </div>

        <div className="auth-summary">
          <span className="field-label">MAL account</span>
          <strong>{isSignedIn ? currentUser.username : 'Not signed in'}</strong>
        </div>

        <div className="action-row auth-actions">
          {!isSignedIn ? (
            <button
              className="primary-button"
              type="button"
              onClick={onLogin}
              disabled={isCheckingSession}
            >
              {isCheckingSession ? 'Checking...' : 'Sign in with MAL'}
            </button>
          ) : (
            <>
              <button
                className="secondary-button"
                type="button"
                onClick={onOpenUserPage}
                disabled={isUserPageOpen || isCheckingSession}
              >
                My page
              </button>
              <button
                className="secondary-button"
                type="button"
                onClick={onRefresh}
                disabled={isLoading || isCheckingSession}
              >
                {isLoading ? 'Loading...' : 'Load my list'}
              </button>
              <button
                className="secondary-button"
                type="button"
                onClick={onSync}
                disabled={isSyncing}
              >
                {isSyncing ? 'Starting...' : 'Sync my list'}
              </button>
              <button
                className="ghost-button"
                type="button"
                onClick={onLogout}
                disabled={isSyncing}
              >
                Sign out
              </button>
            </>
          )}
        </div>
      </div>
    </header>
  )
}

export default UserControls
