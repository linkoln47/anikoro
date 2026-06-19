function UserControls({
  currentUser,
  onLogin,
  onLogout,
  onOpenUserPage,
  onReload,
  isCheckingSession,
  isReloading,
  isUserPageOpen,
}) {
  const isSignedIn = Boolean(currentUser)

  return (
    <header className="auth-strip">
      <div className="auth-strip-inner">
        <div className="auth-strip-title">
          <span className="field-label">anikoro Dashboard</span>
        </div>

        <div className="auth-identity">
          <div className="auth-summary">
            <span className="field-label">MAL account</span>
            <div className="auth-account">
              {isSignedIn ? (
                <button
                  className={`reload-button${isReloading ? ' is-spinning' : ''}`}
                  type="button"
                  onClick={onReload}
                  disabled={isReloading || isCheckingSession}
                  aria-label="Reload my list"
                  title="Reload my list"
                >
                  <svg
                    className="reload-icon"
                    viewBox="0 0 24 24"
                    aria-hidden="true"
                    focusable="false"
                  >
                    <path
                      fill="currentColor"
                      d="M12 4V1L8 5l4 4V6c3.31 0 6 2.69 6 6 0 1.01-.25 1.97-.7 2.8l1.46 1.46C19.54 15.03 20 13.57 20 12c0-4.42-3.58-8-8-8zm0 14c-3.31 0-6-2.69-6-6 0-1.01.25-1.97.7-2.8L5.24 7.74C4.46 8.97 4 10.43 4 12c0 4.42 3.58 8 8 8v3l4-4-4-4v3z"
                    />
                  </svg>
                </button>
              ) : null}
              <strong>{isSignedIn ? currentUser.username : 'Not signed in'}</strong>
            </div>
          </div>
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
                className="ghost-button"
                type="button"
                onClick={onLogout}
                disabled={isReloading}
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
