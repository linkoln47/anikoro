import { useCallback, useEffect, useState } from 'react'
import {
  getCurrentSeason,
  isValidSeasonName,
  isValidSeasonYear,
} from '../entities/season/season'

// Path router for the seasonal browse view. Unlike the hash router used for the
// user and anime detail views, the seasonal page lives at a real path
// (/seasons/{year}/{season}) so it can be linked and refreshed directly. The
// production nginx config and the Vite dev server both fall back to index.html
// for unknown paths, so deep links resolve here on the client.
const SEASON_PATH_PATTERN = /^\/seasons(?:\/(\d{1,4})\/([a-zA-Z]+))?\/?$/

function readPathname() {
  if (typeof window === 'undefined') {
    return '/'
  }

  return window.location.pathname
}

function seasonPath({ year, season }) {
  return `/seasons/${year}/${season}`
}

function parseSeasonFromPath(pathname) {
  const match = pathname.match(SEASON_PATH_PATTERN)
  if (!match) {
    return { isSeasonOpen: false, season: null, needsNormalize: false }
  }

  const year = Number.parseInt(match[1], 10)
  const season = (match[2] ?? '').toLowerCase()

  if (isValidSeasonYear(year) && isValidSeasonName(season)) {
    return { isSeasonOpen: true, season: { year, season }, needsNormalize: false }
  }

  // Bare or invalid /seasons URLs fall back to the current season and get
  // rewritten to a concrete path.
  return { isSeasonOpen: true, season: getCurrentSeason(), needsNormalize: true }
}

export default function useSeasonRoute() {
  const [state, setState] = useState(() => parseSeasonFromPath(readPathname()))

  useEffect(() => {
    if (typeof window === 'undefined') {
      return undefined
    }

    function handlePopState() {
      setState(parseSeasonFromPath(readPathname()))
    }

    window.addEventListener('popstate', handlePopState)

    return () => {
      window.removeEventListener('popstate', handlePopState)
    }
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined' || !state.isSeasonOpen || !state.needsNormalize) {
      return
    }

    window.history.replaceState(null, '', seasonPath(state.season))
    setState({ isSeasonOpen: true, season: state.season, needsNormalize: false })
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
    setState({ isSeasonOpen: true, season: target, needsNormalize: false })
  }, [])

  const closeSeason = useCallback(() => {
    if (typeof window === 'undefined') {
      return
    }

    window.history.pushState(null, '', '/')
    setState({ isSeasonOpen: false, season: null, needsNormalize: false })
  }, [])

  return {
    isSeasonOpen: state.isSeasonOpen,
    season: state.season,
    openSeason,
    closeSeason,
  }
}
