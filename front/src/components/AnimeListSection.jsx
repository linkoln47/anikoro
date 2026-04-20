import { useEffect, useRef, useState } from 'react'

const loadingRows = Array.from({ length: 5 })
const titleCollator = new Intl.Collator('en', {
  sensitivity: 'base',
  numeric: true,
})
const sortableHeaders = [
  { key: 'title', label: 'Anime title', firstDirection: 'asc' },
  { key: 'score', label: 'Score', firstDirection: 'desc' },
  { key: 'merged', label: 'Merged', firstDirection: 'desc' },
  { key: 'watched', label: 'Watched', firstDirection: 'desc' },
  { key: 'syncedAt', label: 'Synced at', firstDirection: 'desc' },
]

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

function hasScore(value) {
  return !Number.isNaN(Number(value))
}

function readScoreValue(value) {
  const numeric = Number(value)
  return Number.isNaN(numeric) ? -1 : numeric
}

function readSyncedAtValue(value) {
  const timestamp = Date.parse(value)
  return Number.isNaN(timestamp) ? 0 : timestamp
}

function readNumericValue(value) {
  const numeric = Number(value)
  return Number.isNaN(numeric) ? 0 : numeric
}

function compareAnime(left, right, key) {
  switch (key) {
    case 'id':
      return left.id - right.id
    case 'title':
      return titleCollator.compare(left.display_title, right.display_title)
    case 'score':
      return readScoreValue(left.avg_score) - readScoreValue(right.avg_score)
    case 'merged':
      return readNumericValue(left.merged_titles) - readNumericValue(right.merged_titles)
    case 'watched':
      return (
        readNumericValue(left.watched_episodes_sum) -
        readNumericValue(right.watched_episodes_sum)
      )
    case 'syncedAt':
      return readSyncedAtValue(left.synced_at) - readSyncedAtValue(right.synced_at)
    default:
      return 0
  }
}

function sortAnime(items, sortState) {
  const sorted = [...items].sort((left, right) => left.id - right.id)

  if (!sortState.key || !sortState.direction) {
    return sorted
  }

  const directionMultiplier = sortState.direction === 'asc' ? 1 : -1

  return sorted.sort((left, right) => {
    const primaryCompare = compareAnime(left, right, sortState.key)
    if (primaryCompare !== 0) {
      return primaryCompare * directionMultiplier
    }

    return left.id - right.id
  })
}

function AnimeTableSkeleton() {
  return (
    <div className="anime-table-shell">
      <table className="anime-table" aria-hidden="true">
        <thead>
          <tr>
            <th>
              <span className="table-header-label">#</span>
            </th>
            {sortableHeaders.map((header) => {
              if (header.key === 'merged') {
                return [
                  <th key="type">
                    <span className="table-header-label">Type</span>
                  </th>,
                  <th key={header.key}>
                    <span className="table-header-label">{header.label}</span>
                  </th>,
                ]
              }

              return (
                <th key={header.key}>
                  <span className="table-header-label">{header.label}</span>
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
              <td className="title-cell">
                <div className="title-block">
                  <span className="skeleton-line skeleton-title-main" />
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
  const filtersMenuRef = useRef(null)
  const [searchQuery, setSearchQuery] = useState('')
  const [typeFilter, setTypeFilter] = useState('all')
  const [scoreFilter, setScoreFilter] = useState('all')
  const [isFiltersOpen, setIsFiltersOpen] = useState(false)
  const [sortState, setSortState] = useState({
    key: null,
    direction: null,
  })

  useEffect(() => {
    setSearchQuery('')
    setTypeFilter('all')
    setScoreFilter('all')
    setIsFiltersOpen(false)
    setSortState({
      key: null,
      direction: null,
    })
  }, [activeUserId])

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

  const normalizedQuery = searchQuery.trim().toLowerCase()
  const filteredAnime = anime.filter((item) => {
    if (typeFilter !== 'all' && item.type !== typeFilter) {
      return false
    }

    if (scoreFilter === 'scored' && !hasScore(item.avg_score)) {
      return false
    }

    if (scoreFilter === 'unscored' && hasScore(item.avg_score)) {
      return false
    }

    if (!normalizedQuery) {
      return true
    }

    const searchableText = `${item.display_title} ${item.id}`.toLowerCase()
    return searchableText.includes(normalizedQuery)
  })

  const visibleAnime = sortAnime(filteredAnime, sortState)
  const hasActiveFilters = typeFilter !== 'all' || scoreFilter !== 'all'
  const activeFilterCount =
    Number(typeFilter !== 'all') + Number(scoreFilter !== 'all')
  const hasNarrowingControls = searchQuery.trim() !== '' || hasActiveFilters

  const listMeta = isLoading
    ? 'Loading entries...'
    : hasNarrowingControls
      ? `${visibleAnime.length} of ${anime.length} entries`
      : `${anime.length} entries`

  function clearFilters() {
    setTypeFilter('all')
    setScoreFilter('all')
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

  return (
    <section className="panel list-panel">
      <div className="section-heading">
        <div>
          <p className="section-eyebrow">Anime List</p>
          <h2>{activeUserId ? `User #${activeUserId}` : 'No user selected'}</h2>
        </div>
        <span className="list-meta">{listMeta}</span>
      </div>

      {activeUserId ? (
        <div className="list-controls">
          {/* Search and filter entry point */}
          <div className="toolbar-row">
            <label className="toolbar-search">
              <span className="field-label">Search</span>
              <input
                className="text-input"
                type="search"
                placeholder="Search by title or anime ID"
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
                        Score lives here for now. Type now works as a quick filter
                        from the table header, and we can add more filters later
                        without changing the main layout.
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
                  </div>
                </div>
              ) : null}
            </div>
          </div>
        </div>
      ) : null}

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
      ) : visibleAnime.length === 0 ? (
        <div className="empty-state">
          No anime matches the current search and filters.
        </div>
      ) : (
        <div className="anime-table-shell">
          <table className="anime-table">
            <thead>
              <tr>
                <th>
                  <span className="table-header-label">#</span>
                </th>
                {sortableHeaders.map((header) => {
                  const isActive = sortState.key === header.key

                  if (header.key === 'merged') {
                    return [
                      <th key="type">
                        <button
                          className="table-sort-button table-filter-button"
                          type="button"
                          onClick={cycleTypeFilter}
                          aria-label={`Type filter: ${
                            typeFilter === 'all'
                              ? 'all anime'
                              : formatTypeLabel(typeFilter)
                          }`}
                        >
                          <span>Type</span>
                          {renderTypeFilterIndicator()}
                        </button>
                      </th>,
                      <th
                        key={header.key}
                        aria-sort={getAriaSort(header.key)}
                      >
                        <button
                          className={`table-sort-button${isActive ? ' is-active' : ''}`}
                          type="button"
                          onClick={() => handleSort(header.key)}
                        >
                          <span>{header.label}</span>
                          {renderSortIndicator(header.key)}
                        </button>
                      </th>,
                    ]
                  }

                  return (
                    <th
                      key={header.key}
                      aria-sort={getAriaSort(header.key)}
                    >
                      <button
                        className={`table-sort-button${isActive ? ' is-active' : ''}`}
                        type="button"
                        onClick={() => handleSort(header.key)}
                      >
                        <span>{header.label}</span>
                        {renderSortIndicator(header.key)}
                      </button>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {visibleAnime.map((item, index) => (
                <tr key={`${item.type}-${item.id}`}>
                  <td className="rank-cell">{index + 1}</td>
                  <td className="title-cell">
                    <div className="title-block">
                      <span className="title-main">{item.display_title}</span>
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
