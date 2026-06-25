import { useEffect, useState } from 'react'
import {
  SEASON_LABELS,
  SEASON_NAMES,
  collectSeasonGenres,
  filterSeasonAnimeByGenres,
  getAdjacentSeason,
  getCurrentSeason,
  sortSeasonAnime,
} from '../entities/season/season'
import SeasonAnimeCard from './SeasonAnimeCard'

const SORT_OPTIONS = [
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
  const [sortKey, setSortKey] = useState('title')
  const [selectedGenreIds, setSelectedGenreIds] = useState([])
  const yearOptions = buildYearOptions()
  const safeAnime = Array.isArray(anime) ? anime : []
  const availableGenres = collectSeasonGenres(safeAnime)
  const filteredAnime = filterSeasonAnimeByGenres(safeAnime, selectedGenreIds)
  const visibleAnime = sortSeasonAnime(filteredAnime, sortKey)
  const seasonLabel = `${SEASON_LABELS[season.season] ?? season.season} ${season.year}`

  // Reset the genre filter when the season changes: its available genres differ,
  // and a stale selection could otherwise filter the new season down to nothing.
  useEffect(() => {
    setSelectedGenreIds([])
  }, [season.year, season.season])

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

        {availableGenres.length > 0 ? (
          <div
            className="season-genre-filter type-filter"
            role="group"
            aria-label="Filter by genre"
          >
            {availableGenres.map((genre) => {
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
        ) : null}

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
            <p className="list-meta season-count">{visibleAnime.length} titles</p>
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
