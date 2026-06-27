import { useEffect, useRef, useState } from 'react'
import {
  SEASON_LABELS,
  SEASON_NAMES,
  collectSeasonGenres,
  filterOutExplicitAnime,
  filterSeasonAnimeByGenres,
  getAdjacentSeason,
  getCurrentSeason,
  groupSeasonGenres,
  sortSeasonAnime,
} from '../entities/season/season'
import SeasonAnimeCard from './SeasonAnimeCard'

const SORT_OPTIONS = [
  { key: 'score', label: 'Score' },
  { key: 'title', label: 'Title' },
  { key: 'date', label: 'Air date' },
  { key: 'episodes', label: 'Episodes' },
]

const EARLIEST_SELECTABLE_YEAR = 1960
const skeletonCards = Array.from({ length: 12 })

function buildYearOptions() {
  const maxYear = getCurrentSeason().year + 1
  const years = []
  for (let year = maxYear; year >= EARLIEST_SELECTABLE_YEAR; year -= 1) {
    years.push(year)
  }

  return years
}

function SeasonPage({ season, anime, isLoading, error, onNavigate, onSelectAnime }) {
  const [sortKey, setSortKey] = useState('score')
  const [selectedGenreIds, setSelectedGenreIds] = useState([])
  // R18+ gate: off by default, so Ecchi/Hentai/Erotica titles stay hidden until the
  // viewer opts in. Kept across season navigation as a viewer-level preference.
  const [showAdult, setShowAdult] = useState(false)
  const [isFilterOpen, setIsFilterOpen] = useState(false)
  const filterRef = useRef(null)
  const yearOptions = buildYearOptions()
  const safeAnime = Array.isArray(anime) ? anime : []
  // Apply the R18+ gate first so the dropdown's genre list and the genre filter only
  // ever see the titles the viewer is allowed to browse.
  const baseAnime = showAdult ? safeAnime : filterOutExplicitAnime(safeAnime)
  const availableGenres = collectSeasonGenres(baseAnime)
  const genreSections = groupSeasonGenres(availableGenres)
  const filteredAnime = filterSeasonAnimeByGenres(baseAnime, selectedGenreIds)
  const visibleAnime = sortSeasonAnime(filteredAnime, sortKey)
  const seasonLabel = `${SEASON_LABELS[season.season] ?? season.season} ${season.year}`

  // Reset the genre filter when the season changes: its available genres differ,
  // and a stale selection could otherwise filter the new season down to nothing.
  // Also close the popover so it never lingers over a different season's controls.
  useEffect(() => {
    setSelectedGenreIds([])
    setIsFilterOpen(false)
  }, [season.year, season.season])

  // Close the filter popover on an outside click or Escape, matching native
  // dropdown behavior without pulling in a popover library.
  useEffect(() => {
    if (!isFilterOpen) {
      return undefined
    }

    function handlePointerDown(event) {
      if (filterRef.current && !filterRef.current.contains(event.target)) {
        setIsFilterOpen(false)
      }
    }

    function handleKeyDown(event) {
      if (event.key === 'Escape') {
        setIsFilterOpen(false)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isFilterOpen])

  function navigateBy(delta) {
    const target = getAdjacentSeason(season, delta)
    onNavigate(target.year, target.season)
  }

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
            <p className="section-eyebrow">Seasonal Anime</p>
            <h1>{seasonLabel}</h1>
          </div>
        </header>

        <div className="season-controls">
          <div className="season-nav">
            <button
              className="ghost-button season-nav-button"
              type="button"
              onClick={() => navigateBy(-1)}
              aria-label="Previous season"
            >
              ‹ Prev
            </button>

            <div className="season-selectors">
              <label className="toolbar-field">
                <span className="field-label">Season</span>
                <select
                  className="select-input"
                  value={season.season}
                  onChange={(event) => onNavigate(season.year, event.target.value)}
                >
                  {SEASON_NAMES.map((name) => (
                    <option key={name} value={name}>
                      {SEASON_LABELS[name]}
                    </option>
                  ))}
                </select>
              </label>

              <label className="toolbar-field">
                <span className="field-label">Year</span>
                <select
                  className="select-input"
                  value={season.year}
                  onChange={(event) => onNavigate(Number(event.target.value), season.season)}
                >
                  {yearOptions.map((year) => (
                    <option key={year} value={year}>
                      {year}
                    </option>
                  ))}
                </select>
              </label>
            </div>

            <button
              className="ghost-button season-nav-button"
              type="button"
              onClick={() => navigateBy(1)}
              aria-label="Next season"
            >
              Next ›
            </button>
          </div>

          <div className="season-tools">
            <button
              type="button"
              className={`season-r18-toggle${showAdult ? ' is-active' : ''}`}
              aria-pressed={showAdult}
              onClick={() => setShowAdult((current) => !current)}
              disabled={isLoading}
              title="Show titles tagged Ecchi, Hentai or Erotica"
            >
              R18+
            </button>

            <div className="season-filter" ref={filterRef}>
              <button
                type="button"
                className={`season-filter-toggle${selectedGenreIds.length > 0 ? ' has-selection' : ''}`}
                aria-haspopup="true"
                aria-expanded={isFilterOpen}
                onClick={() => setIsFilterOpen((current) => !current)}
                disabled={isLoading || availableGenres.length === 0}
              >
                <span>Filter</span>
                {selectedGenreIds.length > 0 ? (
                  <span className="season-filter-count">{selectedGenreIds.length}</span>
                ) : null}
                <span className="season-filter-caret" aria-hidden="true">
                  ▾
                </span>
              </button>

              {isFilterOpen && availableGenres.length > 0 ? (
                <div className="season-filter-menu" role="group" aria-label="Filter by genre">
                  <div className="season-filter-menu-head">
                    <span className="field-label">Genres</span>
                    <button
                      type="button"
                      className="season-filter-clear"
                      onClick={() => setSelectedGenreIds([])}
                      disabled={selectedGenreIds.length === 0}
                    >
                      Clear all
                    </button>
                  </div>
                  {genreSections.map((section) => (
                    <div key={section.key} className="season-filter-section">
                      <p className="season-filter-section-title">{section.label}</p>
                      <div className="type-filter">
                        {section.genres.map((genre) => {
                          const isActive = selectedGenreIds.includes(genre.id)
                          return (
                            <button
                              key={genre.id}
                              type="button"
                              className={`type-filter-button${isActive ? ' is-active' : ''}`}
                              aria-pressed={isActive}
                              onClick={() => toggleGenre(genre.id)}
                            >
                              {genre.name}
                            </button>
                          )
                        })}
                      </div>
                    </div>
                  ))}
                </div>
              ) : null}
            </div>

            <label className="toolbar-field season-sort">
              <span className="field-label">Sort</span>
              <select
                className="select-input"
                value={sortKey}
                onChange={(event) => setSortKey(event.target.value)}
                disabled={isLoading}
              >
                {SORT_OPTIONS.map((option) => (
                  <option key={option.key} value={option.key}>
                    {option.label}
                  </option>
                ))}
              </select>
            </label>
          </div>
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
        ) : visibleAnime.length === 0 ? (
          <div className="empty-state">
            {selectedGenreIds.length > 0 && safeAnime.length > 0
              ? 'No anime match the selected genres.'
              : `No anime stored for ${seasonLabel} yet. Titles appear here once a sync includes anime that premiered this season.`}
          </div>
        ) : (
          <>
            <p className="list-meta season-count">
              {visibleAnime.length} / {safeAnime.length} titles
            </p>
            <div className="season-grid">
              {visibleAnime.map((item) => (
                <SeasonAnimeCard key={item.id} anime={item} onSelect={onSelectAnime} />
              ))}
            </div>
          </>
        )}
      </div>
    </section>
  )
}

export default SeasonPage
