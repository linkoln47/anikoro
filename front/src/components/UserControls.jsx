function UserControls({
  currentUser,
  onLogin,
  onLogout,
  onSync,
  onRefresh,
  isCheckingSession,
  isLoading,
  isSyncing,
}) {
  const isSignedIn = Boolean(currentUser)

  return (
    <div className="controls">
      {/* Session summary */}
      <div className="auth-summary">
        <span className="field-label">MAL account</span>
        <strong>{isSignedIn ? currentUser.username : 'Not signed in'}</strong>
      </div>

      {/* Dashboard actions */}
      <div className="action-row">
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
              className="primary-button"
              type="button"
              onClick={onRefresh}
              disabled={isLoading || isCheckingSession}
            >
              {isLoading ? 'Loading...' : 'Load Data'}
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={onSync}
              disabled={isSyncing}
            >
              {isSyncing ? 'Starting...' : 'Start Sync'}
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
  )
}

export default UserControls
