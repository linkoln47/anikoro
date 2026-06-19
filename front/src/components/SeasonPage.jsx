import { useState } from 'react'
import {
  SEASON_LABELS,
  SEASON_NAMES,
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

function SeasonPage({ season, anime, isLoading, error, onNavigate, onBack, onSelectAnime }) {
  const [sortKey, setSortKey] = useState('title')
  const yearOptions = buildYearOptions()
  const safeAnime = Array.isArray(anime) ? anime : []
  const visibleAnime = sortSeasonAnime(safeAnime, sortKey)
  const seasonLabel = `${SEASON_LABELS[season.season] ?? season.season} ${season.year}`

  function navigateBy(delta) {
    const target = getAdjacentSeason(season, delta)
    onNavigate(target.year, target.season)
  }

  return (
    <section className="season-page">
      <div className="panel season-panel">
        <header className="season-header">
          <div>
            <p className="section-eyebrow">Seasonal Anime</p>
            <h1>{seasonLabel}</h1>
          </div>

          <button className="secondary-button" type="button" onClick={onBack}>
            Back to dashboard
          </button>
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
            No anime stored for {seasonLabel} yet. Titles appear here once a sync includes anime
            that premiered this season.
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
