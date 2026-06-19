import { useCallback, useEffect, useState } from 'react'
import {
  getCurrentSeason,
  isValidSeasonName,
  isValidSeasonYear,
} from '../entities/season/season'

// Path router for the browse views that live at real URLs instead of the hash
// used by the dashboard. It owns two views:
//   - the seasonal grid at /seasons/{year}/{season}
//   - a single franchise page at /franchise/{anime_id}
// Both can be linked and refreshed directly: the production nginx config
// (try_files ... /index.html) and the Vite dev server fall back to index.html
// for unknown paths, so deep links resolve here on the client. Clicking a season
// card pushes a real navigation to the franchise page rather than swapping an
// overlay in place.
const SEASON_PATH_PATTERN = /^\/seasons(?:\/(\d{1,4})\/([a-zA-Z]+))?\/?$/
const FRANCHISE_PATH_PATTERN = /^\/franchise\/([1-9]\d*)\/?$/

function readPathname() {
  if (typeof window === 'undefined') {
    return '/'
  }

  return window.location.pathname
}

function seasonPath({ year, season }) {
  return `/seasons/${year}/${season}`
}

function franchisePath(animeId) {
  return `/franchise/${animeId}`
}

function parsePath(pathname) {
  const franchiseMatch = pathname.match(FRANCHISE_PATH_PATTERN)
  if (franchiseMatch) {
    return {
      view: 'franchise',
      season: null,
      franchiseId: Number.parseInt(franchiseMatch[1], 10),
      needsNormalize: false,
    }
  }

  const seasonMatch = pathname.match(SEASON_PATH_PATTERN)
  if (!seasonMatch) {
    return { view: 'none', season: null, franchiseId: null, needsNormalize: false }
  }

  const year = Number.parseInt(seasonMatch[1], 10)
  const season = (seasonMatch[2] ?? '').toLowerCase()

  if (isValidSeasonYear(year) && isValidSeasonName(season)) {
    return {
      view: 'season',
      season: { year, season },
      franchiseId: null,
      needsNormalize: false,
    }
  }

  // Bare or invalid /seasons URLs fall back to the current season and get
  // rewritten to a concrete path.
  return { view: 'season', season: getCurrentSeason(), franchiseId: null, needsNormalize: true }
}

export default function usePathRoute() {
  const [state, setState] = useState(() => parsePath(readPathname()))

  useEffect(() => {
    if (typeof window === 'undefined') {
      return undefined
    }

    function handlePopState() {
      setState(parsePath(readPathname()))
    }

    window.addEventListener('popstate', handlePopState)

    return () => {
      window.removeEventListener('popstate', handlePopState)
    }
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined' || state.view !== 'season' || !state.needsNormalize) {
      return
    }

    window.history.replaceState(null, '', seasonPath(state.season))
    setState({ view: 'season', season: state.season, franchiseId: null, needsNormalize: false })
  }, [state])

  const openSeason = useCallback((year, season) => {
    if (typeof window === 'undefined') {
      return
    }

    const target =
      isValidSeasonYear(year) && isValidSeasonName(season)
        ? { year, season }
        : getCurrentSeason()

    window.history.pushState(null, '', seasonPath(target))
    setState({ view: 'season', season: target, franchiseId: null, needsNormalize: false })
  }, [])

  const closeSeason = useCallback(() => {
    if (typeof window === 'undefined') {
      return
    }

    window.history.pushState(null, '', '/')
    setState({ view: 'none', season: null, franchiseId: null, needsNormalize: false })
  }, [])

  const resetToDashboard = useCallback(() => {
    if (typeof window === 'undefined') {
      return
    }

    window.history.pushState(null, '', '/')
    setState({ view: 'none', season: null, franchiseId: null, needsNormalize: false })
  }, [])

  const openFranchise = useCallback((animeId) => {
    if (typeof window === 'undefined') {
      return
    }

    const id = Number.parseInt(animeId, 10)
    if (!Number.isInteger(id) || id <= 0) {
      return
    }

    // Mark the entry as internal so the franchise page can step back to the
    // exact view it came from; deep links (no internal flag) resolve below.
    window.history.pushState({ internal: true }, '', franchisePath(id))
    setState({ view: 'franchise', season: null, franchiseId: id, needsNormalize: false })
  }, [])

  const closeFranchise = useCallback(() => {
    if (typeof window === 'undefined') {
      return
    }

    if (window.history.state?.internal) {
      window.history.back()
      return
    }

    // Reached the franchise page via a direct deep link, so there is no prior
    // in-app view to pop back to; land on the current seasonal grid instead.
    const target = getCurrentSeason()
    window.history.pushState(null, '', seasonPath(target))
    setState({ view: 'season', season: target, franchiseId: null, needsNormalize: false })
  }, [])

  return {
    isSeasonOpen: state.view === 'season',
    isFranchiseOpen: state.view === 'franchise',
    season: state.season,
    franchiseId: state.franchiseId,
    openSeason,
    closeSeason,
    resetToDashboard,
    openFranchise,
    closeFranchise,
  }
}
