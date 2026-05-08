const scoreBuckets = Array.from({ length: 10 }, (_, index) => index + 1)
const numberFormatter = new Intl.NumberFormat('en')

function readNumericValue(value) {
  const numeric = Number(value)
  return Number.isFinite(numeric) ? numeric : 0
}

function readScore(value) {
  const numeric = Number(value)
  return Number.isFinite(numeric) && numeric > 0 ? numeric : null
}

function formatScore(value) {
  const numeric = Number(value)
  if (!Number.isFinite(numeric) || numeric <= 0) {
    return '-'
  }

  return Number.isInteger(numeric) ? numeric.toFixed(0) : numeric.toFixed(1)
}

function buildScoreBuckets(anime) {
  const buckets = scoreBuckets.map((score) => ({
    score,
    count: 0,
  }))

  anime.forEach((item) => {
    const score = readScore(item.avg_score)
    if (score === null) {
      return
    }

    const roundedScore = Math.min(10, Math.max(1, Math.round(score)))
    buckets[roundedScore - 1].count += 1
  })

  return buckets
}

function calculateAverageScore(anime) {
  const ratedScores = anime
    .map((item) => readScore(item.avg_score))
    .filter((score) => score !== null)

  if (ratedScores.length === 0) {
    return 0
  }

  return ratedScores.reduce((total, score) => total + score, 0) / ratedScores.length
}

function UserStatCard({ label, value, meta, isLoading }) {
  return (
    <article className="user-stat-card">
      <span className="stat-label">{label}</span>
      {isLoading ? (
        <>
          <span className="skeleton-line skeleton-user-stat-value" aria-hidden="true" />
          <span className="skeleton-line skeleton-user-stat-meta" aria-hidden="true" />
        </>
      ) : (
        <>
          <strong>{value}</strong>
          <span className="user-stat-meta">{meta}</span>
        </>
      )}
    </article>
  )
}

function FranchiseScoreChart({ anime, isLoading }) {
  const buckets = buildScoreBuckets(anime)
  const ratedFranchiseCount = buckets.reduce((total, bucket) => total + bucket.count, 0)
  const maxBucketCount = Math.max(...buckets.map((bucket) => bucket.count), 0)
  const averageScore = calculateAverageScore(anime)

  return (
    <section className="franchise-score-chart" aria-labelledby="franchise-score-chart-title">
      <header className="franchise-score-chart-header">
        <div>
          <span className="stat-label">Franchise ratings</span>
          <h2 id="franchise-score-chart-title">Score distribution</h2>
        </div>
        <div className="franchise-score-chart-summary">
          <strong>{isLoading ? '-' : formatScore(averageScore)}</strong>
          <span>average</span>
        </div>
      </header>

      {isLoading ? (
        <div className="score-chart-skeleton" aria-hidden="true">
          {scoreBuckets.map((score) => (
            <span
              key={score}
              className="skeleton-line score-chart-skeleton-bar"
              style={{ '--bar-height': `${28 + score * 6}%` }}
            />
          ))}
        </div>
      ) : ratedFranchiseCount > 0 ? (
        <div
          className="score-chart-plot"
          role="list"
          aria-label="Franchise counts by rounded average score"
        >
          {buckets.map((bucket) => {
            const barHeight =
              bucket.count === 0 || maxBucketCount === 0
                ? 0
                : Math.max(10, Math.round((bucket.count / maxBucketCount) * 100))

            return (
              <div
                key={bucket.score}
                className="score-chart-column"
                role="listitem"
                tabIndex={bucket.count > 0 ? 0 : undefined}
                aria-label={`${bucket.count} franchises with rounded score ${bucket.score}`}
                style={{
                  '--bar-height': `${barHeight}%`,
                  '--bar-delay': `${bucket.score * 45}ms`,
                }}
              >
                <span className="score-chart-bar-shell" aria-hidden="true">
                  <span className="score-chart-bar">
                    {bucket.count > 0 ? (
                      <span className="score-chart-count">
                        {numberFormatter.format(bucket.count)}
                      </span>
                    ) : null}
                  </span>
                </span>
                <span className="score-chart-label">{bucket.score}</span>
              </div>
            )
          })}
        </div>
      ) : (
        <p className="score-chart-empty">No rated franchises yet.</p>
      )}

      {!isLoading && ratedFranchiseCount > 0 ? (
        <p className="score-chart-caption">
          {numberFormatter.format(ratedFranchiseCount)} rated franchises
        </p>
      ) : null}
    </section>
  )
}

function UserPage({ currentUser, stats, anime, isLoading, isCheckingSession, onBack }) {
  const title = currentUser?.username ?? (isCheckingSession ? 'Loading profile' : 'User page')
  const safeAnime = Array.isArray(anime) ? anime : []
  const totalEpisodes = safeAnime.reduce(
    (total, item) => total + readNumericValue(item.watched_episodes_sum),
    0,
  )
  const ratedFranchiseCount = safeAnime.filter((item) => readScore(item.avg_score) !== null).length
  const averageScore = calculateAverageScore(safeAnime)
  const statCards = [
    {
      label: 'Series',
      value: numberFormatter.format(readNumericValue(stats?.series_count)),
      meta: 'grouped franchises',
    },
    {
      label: 'Movies',
      value: numberFormatter.format(readNumericValue(stats?.movies_count)),
      meta: 'grouped franchises',
    },
    {
      label: 'Total',
      value: numberFormatter.format(readNumericValue(stats?.total_count)),
      meta: 'franchises loaded',
    },
    {
      label: 'Episodes',
      value: numberFormatter.format(totalEpisodes),
      meta: 'watched total',
    },
    {
      label: 'Average score',
      value: formatScore(averageScore),
      meta: 'across rated franchises',
    },
    {
      label: 'Rated',
      value: numberFormatter.format(ratedFranchiseCount),
      meta: 'franchises with score',
    },
  ]

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

        <section className="user-stats-grid" aria-label="User statistics">
          {statCards.map((card) => (
            <UserStatCard
              key={card.label}
              label={card.label}
              value={card.value}
              meta={card.meta}
              isLoading={isLoading}
            />
          ))}
        </section>

        <FranchiseScoreChart anime={safeAnime} isLoading={isLoading} />
      </div>
    </section>
  )
}

export default UserPage
