const loadingRows = Array.from({ length: 5 })

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

function AnimeTableSkeleton() {
  return (
    <div className="anime-table-shell">
      <table className="anime-table" aria-hidden="true">
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
          {loadingRows.map((_, index) => (
            <tr key={index}>
              <td className="rank-cell">
                <span className="skeleton-line skeleton-rank" />
              </td>
              <td className="title-cell">
                <div className="title-block">
                  <span className="skeleton-line skeleton-title-main" />
                  <div className="title-meta">
                    <span className="skeleton-line skeleton-anime-id" />
                    <span className="skeleton-pill" />
                  </div>
                </div>
              </td>
              <td data-label="Score" className="numeric-cell">
                <span className="skeleton-line skeleton-score" />
              </td>
              <td data-label="Type">
                <span className="skeleton-pill" />
              </td>
              <td data-label="Merged" className="numeric-cell">
                <span className="skeleton-line skeleton-compact-value" />
              </td>
              <td data-label="Watched" className="numeric-cell">
                <span className="skeleton-line skeleton-compact-value" />
              </td>
              <td data-label="Synced at" className="synced-cell">
                <span className="skeleton-line skeleton-date" />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function AnimeListSection({ activeUserId, anime, isLoading }) {
  const listMeta = isLoading ? 'Loading entries...' : `${anime.length} entries`

  return (
    <section className="panel list-panel">
      <div className="section-heading">
        <div>
          <p className="section-eyebrow">Anime List</p>
          <h2>{activeUserId ? `User #${activeUserId}` : 'No user selected'}</h2>
        </div>
        <span className="list-meta">{listMeta}</span>
      </div>

      {!activeUserId ? (
        <div className="empty-state">
          Enter a user id and click <strong>Load Data</strong>.
        </div>
      ) : isLoading ? (
        <AnimeTableSkeleton />
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
                        <span className={`type-pill type-pill-${item.type}`}>
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
  )
}

export default AnimeListSection
