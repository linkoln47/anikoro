import { useEffect, useRef, useState } from 'react'
import { franchiseStatusLabels } from '../entities/anime/animeConstants'
import {
  countActiveAnimeFilters,
  filterAnime,
  hasActiveAnimeFilters,
} from '../entities/anime/animeFilters'
import {
  formatAirStart,
  formatScore,
  formatTypeLabel,
} from '../entities/anime/animeFormatters'
import {
  getAirStart,
  getFranchiseStatus,
  getPrimaryAnimeImage,
} from '../entities/anime/animeSelectors'
import { sortAnime } from '../entities/anime/animeSort'

const loadingRows = Array.from({ length: 5 })
const sortableHeaders = [
  { key: 'title', label: 'Anime title', firstDirection: 'asc' },
  { key: 'score', label: 'Score', firstDirection: 'desc' },
  { key: 'merged', label: 'Merged', firstDirection: 'desc' },
  { key: 'watched', label: 'Watched', firstDirection: 'desc' },
  { key: 'airStart', label: 'Air start', firstDirection: 'asc' },
]
const centeredColumnKeys = new Set(['score', 'merged', 'watched'])
const franchiseCoverStatusClasses = {
  completed: 'anime-cover-status-completed',
  watching: 'anime-cover-status-watching',
  on_hold: 'anime-cover-status-on-hold',
  dropped: 'anime-cover-status-dropped',
  plan_to_watch: 'anime-cover-status-plan-to-watch',
}

function getCenteredColumnClass(columnKey) {
  return centeredColumnKeys.has(columnKey) ? 'table-centered-column' : undefined
}

function getCenteredControlClass(columnKey, baseClassName) {
  return centeredColumnKeys.has(columnKey)
    ? `${baseClassName} table-centered-control`
    : baseClassName
}

function CenteredHeaderContent({ label, indicator }) {
  return (
    <span className="table-centered-header-anchor">
      <span>{label}</span>
      {indicator}
    </span>
  )
}

function getCoverClassName(franchiseStatus) {
  return [
    'anime-cover-shell',
    franchiseStatus ? franchiseCoverStatusClasses[franchiseStatus] : '',
  ]
    .filter(Boolean)
    .join(' ')
}

function AnimeTableColGroup() {
  return (
    <colgroup>
      <col className="anime-column-rank" />
      <col className="anime-column-cover" />
      <col className="anime-column-title" />
      <col className="anime-column-score" />
      <col className="anime-column-type" />
      <col className="anime-column-merged" />
      <col className="anime-column-watched" />
      <col className="anime-column-synced" />
    </colgroup>
  )
}

function AnimeTableSkeleton() {
  return (
    <div className="anime-table-shell">
      <table className="anime-table" aria-hidden="true">
        <AnimeTableColGroup />
        <thead>
          <tr>
            <th>
              <span className="table-header-label">#</span>
            </th>
            <th>
              <span className="table-header-label">Cover</span>
            </th>
            {sortableHeaders.map((header) => {
              if (header.key === 'merged') {
                return [
                  <th key="type" className="table-centered-column">
                    <span className="table-header-label table-centered-control">Type</span>
                  </th>,
                  <th key={header.key} className={getCenteredColumnClass(header.key)}>
                    <span className={getCenteredControlClass(header.key, 'table-header-label')}>
                      {header.label}
                    </span>
                  </th>,
                ]
              }

              return (
                <th key={header.key} className={getCenteredColumnClass(header.key)}>
                  <span className={getCenteredControlClass(header.key, 'table-header-label')}>
                    {header.label}
                  </span>
                </th>
              )
            })}
          </tr>
        </thead>
        <tbody>
          {loadingRows.map((_, index) => (
            <tr key={index}>
              <td className="rank-cell">
                <span className="skeleton-line skeleton-rank" />
              </td>
              <td className="cover-cell">
                <span className="skeleton-cover" />
              </td>
              <td className="title-cell">
                <div className="title-block">
                  <span className="skeleton-line skeleton-title-main" />
                </div>
              </td>
              <td data-label="Score" className="numeric-cell table-centered-column">
                <span className="skeleton-line skeleton-score" />
              </td>
              <td data-label="Type" className="table-centered-column">
                <span className="skeleton-pill" />
              </td>
              <td data-label="Merged" className="numeric-cell table-centered-column">
                <span className="skeleton-line skeleton-compact-value" />
              </td>
              <td data-label="Watched" className="numeric-cell table-centered-column">
                <span className="skeleton-line skeleton-compact-value" />
              </td>
              <td data-label="Air start" className="synced-cell">
                <span className="skeleton-line skeleton-date" />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function AnimeListSection({
  activeUsername,
  anime,
  isLoading,
  onSelectAnime = () => {},
}) {
  const filtersMenuRef = useRef(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState('all')
  const [scoreFilter, setScoreFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [isFiltersOpen, setIsFiltersOpen] = useState(false)
  const [sortState, setSortState] = useState({
    key: null,
    direction: null,
  })

  useEffect(() => {
    setSearchQuery('')
    setTypeFilter('all')
    setScoreFilter('all')
    setStatusFilter('all')
    setIsFiltersOpen(false)
    setSortState({
      key: null,
      direction: null,
    })
  }, [activeUsername])

  useEffect(() => {
    if (!isFiltersOpen) {
      return
    }

    function handlePointerDown(event) {
      if (!filtersMenuRef.current?.contains(event.target)) {
        setIsFiltersOpen(false)
      }
    }

    function handleKeyDown(event) {
      if (event.key === 'Escape') {
        setIsFiltersOpen(false)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)

    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isFiltersOpen])

  const filters = {
    searchQuery,
    scoreFilter,
    statusFilter,
    typeFilter,
  }
  const filteredAnime = filterAnime(anime, filters)
  const visibleAnime = sortAnime(filteredAnime, sortState)
  const hasActiveFilters = hasActiveAnimeFilters(filters)
  const activeFilterCount = countActiveAnimeFilters(filters)
  const hasNarrowingControls = searchQuery.trim() !== '' || hasActiveFilters

  const listMeta = isLoading
    ? 'Loading entries...'
    : hasNarrowingControls
      ? `${visibleAnime.length} of ${anime.length} entries`
      : `${anime.length} entries`

  function clearFilters() {
    setTypeFilter('all')
    setScoreFilter('all')
    setStatusFilter('all')
  }

  function cycleTypeFilter() {
    setTypeFilter((current) => {
      if (current === 'all') {
        return 'series'
      }

      if (current === 'series') {
        return 'movie'
      }

      return 'all'
    })
  }

  function handleSort(columnKey) {
    const header = sortableHeaders.find((item) => item.key === columnKey)
    if (!header) {
      return
    }

    setSortState((current) => {
      if (current.key !== columnKey) {
        return {
          key: columnKey,
          direction: header.firstDirection,
        }
      }

      if (current.direction === header.firstDirection) {
        return {
          key: columnKey,
          direction: header.firstDirection === 'asc' ? 'desc' : 'asc',
        }
      }

      return {
        key: null,
        direction: null,
      }
    })
  }

  function getAriaSort(columnKey) {
    if (sortState.key !== columnKey) {
      return 'none'
    }

    return sortState.direction === 'asc' ? 'ascending' : 'descending'
  }

  function renderSortIndicator(columnKey) {
    if (sortState.key !== columnKey) {
      return <span className="sort-indicator" aria-hidden="true" />
    }

    return (
      <span className="sort-indicator is-active" aria-hidden="true">
        {sortState.direction === 'asc' ? '▲' : '▼'}
      </span>
    )
  }

  function renderTypeFilterIndicator() {
    if (typeFilter === 'series') {
      return (
        <span
          className="type-filter-indicator type-filter-indicator-series"
          aria-hidden="true"
        >
          S
        </span>
      )
    }

    if (typeFilter === 'movie') {
      return (
        <span
          className="type-filter-indicator type-filter-indicator-movie"
          aria-hidden="true"
        >
          M
        </span>
      )
    }

    return <span className="type-filter-indicator" aria-hidden="true" />
  }

  function renderSortableHeaderContent(header) {
    if (!centeredColumnKeys.has(header.key)) {
      return (
        <>
          <span>{header.label}</span>
          {renderSortIndicator(header.key)}
        </>
      )
    }

    return (
      <CenteredHeaderContent
        label={header.label}
        indicator={renderSortIndicator(header.key)}
      />
    )
  }

  return (
    <section className="panel list-panel">
      <div className="section-heading">
        <div>
          <p className="section-eyebrow">Anime List</p>
          <h2>{activeUsername ? activeUsername : 'No user selected'}</h2>
        </div>
        <span className="list-meta">{listMeta}</span>
      </div>

      {activeUsername ? (
        <div className="list-controls">
          {/* Search and filter entry point */}
          <div className="toolbar-row">
            <label className="toolbar-search">
              <span className="field-label">Search</span>
              <input
                className="text-input"
                type="search"
                placeholder="Search by title"
                value={searchQuery}
                onChange={(event) => setSearchQuery(event.target.value)}
                disabled={isLoading}
              />
            </label>

            <div className="filters-shell" ref={filtersMenuRef}>
              <span className="field-label">Filters</span>
              <button
                className={`filter-trigger${isFiltersOpen ? ' is-open' : ''}${
                  hasActiveFilters ? ' is-active' : ''
                }`}
                type="button"
                onClick={() => setIsFiltersOpen((current) => !current)}
                disabled={isLoading}
                aria-expanded={isFiltersOpen}
                aria-haspopup="dialog"
              >
                <span className="filter-trigger-main">
                  <span>Filters</span>
                  {activeFilterCount > 0 ? (
                    <span className="filter-count">{activeFilterCount}</span>
                  ) : null}
                </span>
                <span className="filter-chevron" aria-hidden="true">
                  ▾
                </span>
              </button>

              {isFiltersOpen ? (
                <div className="filters-popover" role="dialog" aria-label="Anime filters">
                  <div className="filters-popover-header">
                    <div>
                      <p className="filters-title">Refine anime list</p>
                      <p className="filters-copy">
                        Narrow the current list by score and franchise status.
                      </p>
                    </div>

                    <button
                      className="ghost-button filters-clear-button"
                      type="button"
                      onClick={clearFilters}
                      disabled={!hasActiveFilters}
                    >
                      Clear
                    </button>
                  </div>

                  <div className="filters-grid">
                    <label className="toolbar-field">
                      <span className="field-label">Score</span>
                      <select
                        className="select-input"
                        value={scoreFilter}
                        onChange={(event) => setScoreFilter(event.target.value)}
                        disabled={isLoading}
                      >
                        <option value="all">Any score</option>
                        <option value="scored">Scored only</option>
                        <option value="unscored">Unscored only</option>
                      </select>
                    </label>

                    <label className="toolbar-field">
                      <span className="field-label">Status</span>
                      <select
                        className="select-input"
                        value={statusFilter}
                        onChange={(event) => setStatusFilter(event.target.value)}
                        disabled={isLoading}
                      >
                        {Object.entries(franchiseStatusLabels).map(([value, label]) => (
                          <option key={value} value={value}>
                            {label}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}

      {!activeUsername ? (
        <div className="empty-state">
          Search an anikoro username or sign in.
        </div>
      ) : isLoading ? (
        <AnimeTableSkeleton />
      ) : anime.length === 0 ? (
        <div className="empty-state">
          No grouped anime entries yet. Run sync and refresh this page.
        </div>
      ) : visibleAnime.length === 0 ? (
        <div className="empty-state">
          No anime matches the current search and filters.
        </div>
      ) : (
        <div className="anime-table-shell">
          <table className="anime-table">
            <AnimeTableColGroup />
            <thead>
              <tr>
                <th>
                  <span className="table-header-label">#</span>
                </th>
                <th>
                  <span className="table-header-label">Cover</span>
                </th>
                {sortableHeaders.map((header) => {
                  const isActive = sortState.key === header.key

                  if (header.key === 'merged') {
                    return [
                      <th key="type" className="table-centered-column">
                        <button
                          className="table-sort-button table-filter-button table-centered-control"
                          type="button"
                          onClick={cycleTypeFilter}
                          aria-label={`Type filter: ${
                            typeFilter === 'all'
                              ? 'all anime'
                              : formatTypeLabel(typeFilter)
                          }`}
                        >
                          <CenteredHeaderContent
                            label="Type"
                            indicator={renderTypeFilterIndicator()}
                          />
                        </button>
                      </th>,
                      <th
                        key={header.key}
                        className={getCenteredColumnClass(header.key)}
                        aria-sort={getAriaSort(header.key)}
                      >
                        <button
                          className={`${getCenteredControlClass(
                            header.key,
                            'table-sort-button',
                          )}${isActive ? ' is-active' : ''}`}
                          type="button"
                          onClick={() => handleSort(header.key)}
                        >
                          {renderSortableHeaderContent(header)}
                        </button>
                      </th>,
                    ]
                  }

                  return (
                    <th
                      key={header.key}
                      className={getCenteredColumnClass(header.key)}
                      aria-sort={getAriaSort(header.key)}
                    >
                      <button
                        className={`${getCenteredControlClass(
                          header.key,
                          'table-sort-button',
                        )}${isActive ? ' is-active' : ''}`}
                        type="button"
                        onClick={() => handleSort(header.key)}
                      >
                        {renderSortableHeaderContent(header)}
                      </button>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {visibleAnime.map((item, index) => {
                const imageUrl = getPrimaryAnimeImage(item)
                const airStart = getAirStart(item)
                const franchiseStatus = getFranchiseStatus(item)

                return (
                  <tr
                    key={`${item.type}-${item.id}`}
                    className="anime-table-row is-clickable"
                    tabIndex={0}
                    onClick={() => onSelectAnime(item.id)}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault()
                        onSelectAnime(item.id)
                      }
                    }}
                    aria-label={`Open franchise view for ${item.display_title}`}
                  >
                    <td className="rank-cell">{index + 1}</td>
                    <td data-label="Cover" className="cover-cell">
                      <div
                        className={getCoverClassName(franchiseStatus)}
                        aria-hidden="true"
                      >
                        {imageUrl ? (
                          <img className="anime-cover-image" src={imageUrl} alt="" />
                        ) : (
                          <div className="anime-cover-fallback" />
                        )}
                      </div>
                    </td>
                    <td className="title-cell">
                      <div className="title-block">
                        <span className="title-main">{item.display_title}</span>
                        <div className="title-meta">
                          <span className="title-hint">Open franchise view</span>
                        </div>
                      </div>
                    </td>
                    <td data-label="Score" className="numeric-cell table-centered-column">
                      {formatScore(item.avg_score)}
                    </td>
                    <td data-label="Type" className="table-centered-column">
                      <span className={`type-badge type-${item.type}`}>
                        {formatTypeLabel(item.type)}
                      </span>
                    </td>
                    <td data-label="Merged" className="numeric-cell table-centered-column">
                      {item.merged_titles}
                    </td>
                    <td data-label="Watched" className="numeric-cell table-centered-column">
                      {item.watched_episodes_sum}
                    </td>
                    <td data-label="Air start" className="synced-cell">
                      {formatAirStart(airStart)}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

export default AnimeListSection
