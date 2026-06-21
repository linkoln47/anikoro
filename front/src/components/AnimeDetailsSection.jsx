import { useEffect, useRef } from 'react'
import {
  formatMediaType,
  formatScore,
  formatStartDate,
  formatSyncedAt,
  formatTypeLabel,
} from '../entities/anime/animeFormatters'
import { getPrimaryFranchiseItem } from '../entities/anime/animeSelectors'
import FranchiseEntryEditor from './FranchiseEntryEditor'
import FranchiseEntryStats from './FranchiseEntryStats'

const franchiseStatusClasses = {
  completed: 'franchise-status-completed',
  watching: 'franchise-status-watching',
  on_hold: 'franchise-status-on-hold',
  dropped: 'franchise-status-dropped',
  plan_to_watch: 'franchise-status-plan-to-watch',
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
  backLabel = 'Back to anime list',
  canEditList = false,
  pendingAnimeIds,
  onUpdateListEntry,
  onRemoveListEntry,
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
          {backLabel}
        </button>
        <div className="empty-state">
          Search an anikoro username first, then open a franchise page from the anime list.
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
          {backLabel}
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
          {backLabel}
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

                    <FranchiseEntryStats
                      item={item}
                      canEdit={canEditList}
                      isPending={Boolean(pendingAnimeIds?.has(item.id))}
                      onUpdateEntry={onUpdateListEntry}
                    />
                  </div>

                  {canEditList ? (
                    <FranchiseEntryEditor
                      item={item}
                      isPending={Boolean(pendingAnimeIds?.has(item.id))}
                      onUpdateEntry={onUpdateListEntry}
                      onRemoveEntry={onRemoveListEntry}
                    />
                  ) : null}
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
