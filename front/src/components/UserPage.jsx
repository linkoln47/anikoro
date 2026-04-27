const statPlaceholders = [
  'Series',
  'Movies',
  'Total',
  'Episodes',
  'Average score',
  'Favorites',
]

function UserPage({ currentUser, isCheckingSession, onBack }) {
  const title = currentUser?.username ?? (isCheckingSession ? 'Loading profile' : 'User page')

  return (
    <section className="user-page">
      <div className="panel user-page-panel">
        <header className="user-page-header">
          <div>
            <p className="section-eyebrow">User Page</p>
            <h1>{title}</h1>
          </div>

          <button className="secondary-button" type="button" onClick={onBack}>
            Back to dashboard
          </button>
        </header>

        <section className="user-stats-placeholder-grid" aria-label="User statistics">
          {statPlaceholders.map((label) => (
            <article key={label} className="user-stat-placeholder">
              <span className="stat-label">{label}</span>
              <span className="skeleton-line skeleton-user-stat-value" aria-hidden="true" />
              <span className="skeleton-line skeleton-user-stat-meta" aria-hidden="true" />
            </article>
          ))}
        </section>
      </div>
    </section>
  )
}

export default UserPage
