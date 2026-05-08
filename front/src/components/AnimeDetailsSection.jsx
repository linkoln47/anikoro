import { useEffect, useRef } from 'react'

const mediaTypeLabels = {
  tv: 'TV',
  movie: 'Movie',
  ova: 'OVA',
  ona: 'ONA',
  special: 'Special',
  music: 'Music',
}
const franchiseStatusClasses = {
  completed: 'franchise-status-completed',
  watching: 'franchise-status-watching',
  on_hold: 'franchise-status-on-hold',
  dropped: 'franchise-status-dropped',
  plan_to_watch: 'franchise-status-plan-to-watch',
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

function formatMediaType(value) {
  if (!value) {
    return 'Unknown type'
  }

  return mediaTypeLabels[value] ?? value.replace(/_/g, ' ')
}

function formatScore(value) {
  const numeric = Number(value)
  if (Number.isNaN(numeric) || numeric <= 0) {
    return '-'
  }

  return Number.isInteger(numeric) ? numeric.toFixed(0) : numeric.toFixed(1)
}

function formatStartDate(value) {
  if (!value) {
    return 'Unknown start'
  }

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }

  return new Intl.DateTimeFormat('en', {
    dateStyle: 'medium',
  }).format(date)
}

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

function readNumericValue(value) {
  const numeric = Number(value)
  return Number.isNaN(numeric) ? 0 : numeric
}

function hasAnimeImage(item) {
  return Boolean(item?.image_medium_url || item?.image_large_url)
}

function hasUserWatchedItem(item) {
  return item?.in_user_list && readNumericValue(item.watched_episodes) > 0
}

function getPrimaryFranchiseItem(franchiseItems, selectedAnimeId) {
  const earliestWatchedItem = franchiseItems
    .filter((item) => hasUserWatchedItem(item) && hasAnimeImage(item))
    .sort((left, right) => readNumericValue(left.id) - readNumericValue(right.id))[0]

  return (
    earliestWatchedItem ??
    franchiseItems.find((item) => item.id === selectedAnimeId) ??
    franchiseItems[0] ??
    null
  )
}

function getFranchiseCardClassName(item, selectedAnimeId) {
  return [
    'franchise-card',
    item.in_user_list ? 'is-owned' : '',
    item.id === selectedAnimeId ? 'is-selected' : '',
    item.in_user_list ? franchiseStatusClasses[item.user_list_status] : '',
  ]
    .filter(Boolean)
    .join(' ')
}

function AnimeDetailsSection({
  activeUsername,
  anime,
  selectedAnimeId,
  isLoading,
  onBack,
}) {
  const backButtonRef = useRef(null)
  const selectedAnime =
    anime.find((item) => item.id === selectedAnimeId) ?? null

  useEffect(() => {
    backButtonRef.current?.focus()
  }, [selectedAnimeId])

  if (!activeUsername) {
    return (
      <section className="panel details-panel">
        <button
          ref={backButtonRef}
          className="ghost-button details-back-button"
          type="button"
          onClick={onBack}
        >
          Back to anime list
        </button>
        <div className="empty-state">
          Search a MAL username first, then open a franchise page from the anime list.
        </div>
      </section>
    )
  }

  if (isLoading && anime.length === 0) {
    return (
      <section className="panel details-panel">
        <button
          ref={backButtonRef}
          className="ghost-button details-back-button"
          type="button"
          onClick={onBack}
        >
          Back to anime list
        </button>
        <div className="empty-state">Loading franchise details...</div>
      </section>
    )
  }

  if (!selectedAnime) {
    return (
      <section className="panel details-panel">
        <button
          ref={backButtonRef}
          className="ghost-button details-back-button"
          type="button"
          onClick={onBack}
        >
          Back to anime list
        </button>
        <div className="empty-state">
          Anime group #{selectedAnimeId} is not present in the loaded data for{' '}
          {activeUsername}.
        </div>
      </section>
    )
  }

  const franchiseItems = selectedAnime.franchise ?? []
  const heroItem = getPrimaryFranchiseItem(franchiseItems, selectedAnime.id)
  const heroImageUrl =
    heroItem?.image_large_url || heroItem?.image_medium_url || ''
  const inUserListCount = franchiseItems.filter((item) => item.in_user_list).length
  const summaryCards = [
    { label: 'Related anime', value: franchiseItems.length },
    { label: 'In your list', value: inUserListCount },
    { label: 'Watched episodes', value: selectedAnime.watched_episodes_sum },
    { label: 'Avg score', value: formatScore(selectedAnime.avg_score) },
  ]

  return (
    <section className="panel details-panel">
      <button
        ref={backButtonRef}
        className="ghost-button details-back-button"
        type="button"
        onClick={onBack}
      >
        Back to anime list
      </button>

      <div className="details-hero">
        <div className="details-poster-shell">
          {heroImageUrl ? (
            <img
              className="details-poster"
              src={heroImageUrl}
              alt={heroItem?.title || selectedAnime.display_title}
            />
          ) : (
            <div className="details-poster-fallback">No cover</div>
          )}
        </div>

        <div className="details-copy">
          <p className="section-eyebrow">Franchise Page</p>
          <h2>{selectedAnime.display_title}</h2>
          <p className="details-lead">
            All related anime for this grouped entry, expanded from the MAL
            franchise graph already stored by the backend.
          </p>
          <div className="details-badges">
            <span className={`type-badge type-${selectedAnime.type}`}>
              {formatTypeLabel(selectedAnime.type)}
            </span>
            <span className="info-pill">Last sync {formatSyncedAt(selectedAnime.synced_at)}</span>
            {heroItem?.media_type ? (
              <span className="info-pill">{formatMediaType(heroItem.media_type)}</span>
            ) : null}
          </div>
        </div>
      </div>

      <div className="details-summary-grid">
        {summaryCards.map((card) => (
          <article key={card.label} className="details-summary-card">
            <span className="stat-label">{card.label}</span>
            <strong>{card.value}</strong>
          </article>
        ))}
      </div>

      {franchiseItems.length === 0 ? (
        <div className="empty-state">
          No franchise items are available for this anime yet. Run sync again if you
          expected related titles here.
        </div>
      ) : (
        <div className="franchise-grid">
          {franchiseItems.map((item) => {
            const imageUrl = item.image_large_url || item.image_medium_url || ''
            const stateLabel =
              item.id === selectedAnime.id
                ? 'Selected entry'
                : item.in_user_list
                  ? 'In your list'
                  : 'Related title'

            return (
              <article
                key={item.id}
                className={getFranchiseCardClassName(item, selectedAnime.id)}
              >
                <div className="franchise-card-media">
                  {imageUrl ? (
                    <img
                      className="franchise-card-image"
                      src={imageUrl}
                      alt={item.title || 'Franchise cover'}
                    />
                  ) : (
                    <div className="franchise-card-fallback">No image</div>
                  )}
                </div>

                <div className="franchise-card-body">
                  <p className="franchise-card-kicker">{stateLabel}</p>
                  <h3>{item.title || 'Untitled anime'}</h3>

                  <div className="franchise-card-footer">
                    <div className="franchise-card-tags">
                      {item.media_type ? (
                        <span className="info-pill">{formatMediaType(item.media_type)}</span>
                      ) : null}
                      {item.start_date ? (
                        <span className="info-pill">{formatStartDate(item.start_date)}</span>
                      ) : null}
                      {item.relation_type_formatted ? (
                        <span className="info-pill info-pill-accent">
                          {item.relation_type_formatted}
                        </span>
                      ) : null}
                    </div>

                    <dl className="franchise-card-stats">
                      <div>
                        <dt>User score</dt>
                        <dd>{item.in_user_list ? formatScore(item.user_score) : '-'}</dd>
                      </div>
                      <div>
                        <dt>Watched eps</dt>
                        <dd>{item.in_user_list ? item.watched_episodes : '-'}</dd>
                      </div>
                    </dl>
                  </div>
                </div>
              </article>
            )
          })}
        </div>
      )}
    </section>
  )
}

export default AnimeDetailsSection
