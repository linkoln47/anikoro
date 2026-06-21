import { useMemo, useState } from 'react'
import SeasonAnimeCard from './SeasonAnimeCard'

const skeletonCards = Array.from({ length: 12 })

// The catalog-wide franchise grid behind the "All anime" nav tab. It reuses the
// seasonal grid layout and card so a franchise group reads the same as a season
// entry; selecting a card opens the existing single-franchise page.
function AllAnimePage({ franchises, isLoading, error, onSelectFranchise }) {
  const [query, setQuery] = useState('')
  const safeFranchises = Array.isArray(franchises) ? franchises : []

  const visibleFranchises = useMemo(() => {
    const term = query.trim().toLowerCase()
    if (!term) {
      return safeFranchises
    }

    return safeFranchises.filter((item) =>
      (item.title || '').toLowerCase().includes(term),
    )
  }, [safeFranchises, query])

  return (
    <section className="season-page">
      <div className="panel season-panel">
        <header className="season-header">
          <div>
            <p className="section-eyebrow">Anime Catalog</p>
            <h1>All anime</h1>
          </div>
        </header>

        <div className="season-controls">
          <label className="toolbar-field">
            <span className="field-label">Search</span>
            <input
              className="text-input"
              type="search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Filter by title"
              disabled={isLoading}
            />
          </label>
        </div>

        {error ? (
          <div className="empty-state">{error}</div>
        ) : isLoading ? (
          <div className="season-grid" aria-hidden="true">
            {skeletonCards.map((_, index) => (
              <div key={index} className="season-card is-skeleton">
                <div className="season-card-cover" />
                <div className="season-card-body">
                  <span className="skeleton-line skeleton-title-main" />
                </div>
              </div>
            ))}
          </div>
        ) : visibleFranchises.length === 0 ? (
          <div className="empty-state">
            {safeFranchises.length === 0
              ? 'No anime in the catalog yet. Titles appear here once a sync populates the catalog.'
              : 'No franchises match your search.'}
          </div>
        ) : (
          <>
            <p className="list-meta season-count">{visibleFranchises.length} franchises</p>
            <div className="season-grid">
              {visibleFranchises.map((item) => (
                <SeasonAnimeCard key={item.id} anime={item} onSelect={onSelectFranchise} />
              ))}
            </div>
          </>
        )}
      </div>
    </section>
  )
}

export default AllAnimePage
