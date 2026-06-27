import { useState } from 'react'
import { VirtuosoGrid } from 'react-virtuoso'
import useFranchises from '../features/franchise/useFranchises'
import useGenres from '../features/franchise/useGenres'
import { CATALOG_SORT_OPTIONS, MEDIA_TYPE_FILTERS } from '../entities/anime/animeConstants'
import { filterOutExplicitGenres } from '../entities/season/season'
import GenreFilterDropdown from './GenreFilterDropdown'
import SeasonAnimeCard from './SeasonAnimeCard'

const skeletonCards = Array.from({ length: 12 })

// Module-level so VirtuosoGrid sees a stable component identity across renders.
// The footer renders the "loading more" hint while the next page is in flight,
// reading the flag from the grid context.
function GridFooter({ context }) {
  if (!context?.isLoadingMore) {
    return null
  }
  return <p className="list-meta season-loading-more">Loading more…</p>
}

const gridComponents = { Footer: GridFooter }

// The catalog-wide franchise grid behind the "All anime" nav tab. It reuses the
// seasonal grid layout and card so a franchise group reads the same as a season
// entry; selecting a card opens the existing single-franchise page. The grid is
// virtualized and loads one server page at a time, so it stays responsive even
// across a catalog of thousands of titles; the media-type panel, search box, genre
// filter, R18+ gate, and sort all filter on the server so they cover the whole
// catalog, not just what is loaded.
function AllAnimePage({ onSelectFranchise }) {
  const [mediaType, setMediaType] = useState('')
  const [query, setQuery] = useState('')
  const [sortKey, setSortKey] = useState('score')
  const [selectedGenreIds, setSelectedGenreIds] = useState([])
  const [showAdult, setShowAdult] = useState(false)

  const catalogGenres = useGenres()
  // Mirror the seasonal view: hide the Explicit section unless R18+ is enabled.
  const genreOptions = showAdult ? catalogGenres : filterOutExplicitGenres(catalogGenres)

  const { items, total, isLoading, isLoadingMore, error, loadMore } = useFranchises({
    mediaType,
    search: query.trim(),
    sort: sortKey,
    genreIds: selectedGenreIds,
    includeAdult: showAdult,
  })

  const hasActiveFilter =
    mediaType !== '' || query.trim() !== '' || selectedGenreIds.length > 0

  function toggleGenre(genreId) {
    setSelectedGenreIds((current) =>
      current.includes(genreId)
        ? current.filter((id) => id !== genreId)
        : [...current, genreId],
    )
  }

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
          <label className="toolbar-field season-search">
            <span className="field-label">Search</span>
            <input
              className="text-input"
              type="search"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder="Filter by title"
            />
          </label>

          <div className="season-tools">
            <button
              type="button"
              className={`season-r18-toggle${showAdult ? ' is-active' : ''}`}
              aria-pressed={showAdult}
              onClick={() => setShowAdult((current) => !current)}
              title="Show titles tagged Ecchi, Hentai or Erotica"
            >
              R18+
            </button>

            <GenreFilterDropdown
              genres={genreOptions}
              selectedIds={selectedGenreIds}
              onToggle={toggleGenre}
              onClear={() => setSelectedGenreIds([])}
            />

            <label className="toolbar-field season-sort">
              <span className="field-label">Sort</span>
              <select
                className="select-input"
                value={sortKey}
                onChange={(event) => setSortKey(event.target.value)}
              >
                {CATALOG_SORT_OPTIONS.map((option) => (
                  <option key={option.key} value={option.key}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>

        <div className="type-filter season-media-filter" role="group" aria-label="Filter by type">
          {MEDIA_TYPE_FILTERS.map((filter) => (
            <button
              key={filter.value || 'all'}
              type="button"
              className={`type-filter-button${mediaType === filter.value ? ' is-active' : ''}`}
              aria-pressed={mediaType === filter.value}
              onClick={() => setMediaType(filter.value)}
            >
              {filter.label}
            </button>
          ))}
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
        ) : items.length === 0 ? (
          <div className="empty-state">
            {hasActiveFilter
              ? 'No franchises match your filters.'
              : 'No anime in the catalog yet. Titles appear here once a sync populates the catalog.'}
          </div>
        ) : (
          <>
            <p className="list-meta season-count">
              {items.length} / {total} franchises
            </p>
            <VirtuosoGrid
              useWindowScroll
              data={items}
              endReached={loadMore}
              overscan={600}
              listClassName="season-grid"
              context={{ isLoadingMore }}
              components={gridComponents}
              itemContent={(_, item) => (
                <SeasonAnimeCard anime={item} onSelect={onSelectFranchise} />
              )}
            />
          </>
        )}
      </div>
    </section>
  )
}

export default AllAnimePage
