import { useEffect, useState } from 'react'
import {
  SEASON_LABELS,
  SEASON_NAMES,
  collectSeasonGenres,
  filterOutExplicitAnime,
  filterSeasonAnimeByGenres,
  filterSeasonAnimeByMediaType,
  getAdjacentSeason,
  getCurrentSeason,
  sortSeasonAnime,
} from '../entities/season/season'
import { CATALOG_SORT_OPTIONS, MEDIA_TYPE_FILTERS } from '../entities/anime/animeConstants'
import GenreFilterDropdown from './GenreFilterDropdown'
import SeasonAnimeCard from './SeasonAnimeCard'

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
  const [mediaType, setMediaType] = useState('')
  // R18+ gate: off by default, so Ecchi/Hentai/Erotica titles stay hidden until the
  // viewer opts in. Kept across season navigation as a viewer-level preference.
  const [showAdult, setShowAdult] = useState(false)
  const yearOptions = buildYearOptions()
  const safeAnime = Array.isArray(anime) ? anime : []
  // Pipeline: R18+ gate first, then the media-type filter, so the genre dropdown and
  // the genre filter only ever see the titles the viewer is currently browsing.
  const baseAnime = showAdult ? safeAnime : filterOutExplicitAnime(safeAnime)
  const typedAnime = filterSeasonAnimeByMediaType(baseAnime, mediaType)
  const availableGenres = collectSeasonGenres(typedAnime)
  const filteredAnime = filterSeasonAnimeByGenres(typedAnime, selectedGenreIds)
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

            <GenreFilterDropdown
              genres={availableGenres}
              selectedIds={selectedGenreIds}
              onToggle={toggleGenre}
              onClear={() => setSelectedGenreIds([])}
              disabled={isLoading}
            />

            <label className="toolbar-field season-sort">
              <span className="field-label">Sort</span>
              <select
                className="select-input"
                value={sortKey}
                onChange={(event) => setSortKey(event.target.value)}
                disabled={isLoading}
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

        {!error && !isLoading && safeAnime.length > 0 ? (
          <div className="type-filter season-media-filter" role="group" aria-label="Filter by media type">
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
            {(selectedGenreIds.length > 0 || mediaType !== '') && safeAnime.length > 0
              ? 'No anime match the selected filters.'
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
