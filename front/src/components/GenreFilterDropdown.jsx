import { useEffect, useRef, useState } from 'react'
import { groupSeasonGenres } from '../entities/season/season'

// GenreFilterDropdown is the collapsible "Filter" plaque shared by the seasonal
// grid and the "all franchises" grid. It is controlled: the parent owns the
// `selectedIds` selection (so each page keeps independent state) and supplies the
// available `genres`; this component only owns its open/close state plus the
// outside-click/Escape dismissal. Genres are grouped into sections
// (Genres / Explicit Genres / Themes / Demographics) via groupSeasonGenres.
function GenreFilterDropdown({
  genres,
  selectedIds,
  onToggle,
  onClear,
  disabled = false,
  label = 'Filter',
}) {
  const [isOpen, setIsOpen] = useState(false)
  const containerRef = useRef(null)

  const availableGenres = Array.isArray(genres) ? genres : []
  const selected = Array.isArray(selectedIds) ? selectedIds : []
  const sections = groupSeasonGenres(availableGenres)
  const isDisabled = disabled || availableGenres.length === 0

  // Close on outside click or Escape, matching native dropdown behavior without a
  // popover library.
  useEffect(() => {
    if (!isOpen) {
      return undefined
    }

    function handlePointerDown(event) {
      if (containerRef.current && !containerRef.current.contains(event.target)) {
        setIsOpen(false)
      }
    }

    function handleKeyDown(event) {
      if (event.key === 'Escape') {
        setIsOpen(false)
      }
    }

    document.addEventListener('mousedown', handlePointerDown)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handlePointerDown)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen])

  // Never stay open once the control becomes disabled (e.g. the page started
  // loading or the genre list emptied out).
  useEffect(() => {
    if (isDisabled) {
      setIsOpen(false)
    }
  }, [isDisabled])

  return (
    <div className="season-filter" ref={containerRef}>
      <button
        type="button"
        className={`season-filter-toggle${selected.length > 0 ? ' has-selection' : ''}`}
        aria-haspopup="true"
        aria-expanded={isOpen}
        onClick={() => setIsOpen((current) => !current)}
        disabled={isDisabled}
      >
        <span>{label}</span>
        {selected.length > 0 ? (
          <span className="season-filter-count">{selected.length}</span>
        ) : null}
        <span className="season-filter-caret" aria-hidden="true">
          ▾
        </span>
      </button>

      {isOpen && availableGenres.length > 0 ? (
        <div className="season-filter-menu" role="group" aria-label="Filter by genre">
          <div className="season-filter-menu-head">
            <button
              type="button"
              className="season-filter-clear"
              onClick={() => onClear()}
              disabled={selected.length === 0}
            >
              Clear all
            </button>
          </div>
          {sections.map((section) => (
            <div key={section.key} className="season-filter-section">
              <p className="season-filter-section-title">{section.label}</p>
              <div className="type-filter">
                {section.genres.map((genre) => {
                  const isActive = selected.includes(genre.id)
                  return (
                    <button
                      key={genre.id}
                      type="button"
                      className={`type-filter-button${isActive ? ' is-active' : ''}`}
                      aria-pressed={isActive}
                      onClick={() => onToggle(genre.id)}
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
  )
}

export default GenreFilterDropdown
