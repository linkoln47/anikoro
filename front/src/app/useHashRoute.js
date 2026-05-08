import { useCallback, useEffect, useState } from 'react'

function readSelectedAnimeId() {
  if (typeof window === 'undefined') {
    return null
  }

  const match = window.location.hash.match(/^#\/anime\/([1-9]\d*)$/)
  if (!match) {
    return null
  }

  return Number(match[1])
}

function readIsUserPageOpen() {
  if (typeof window === 'undefined') {
    return false
  }

  return window.location.hash === '#/user'
}

function readHashRoute() {
  return {
    isUserPageOpen: readIsUserPageOpen(),
    selectedAnimeId: readSelectedAnimeId(),
  }
}

function clearRoute() {
  if (typeof window === 'undefined') {
    return
  }

  window.history.replaceState(
    null,
    '',
    `${window.location.pathname}${window.location.search}`,
  )
}

export default function useHashRoute() {
  const [route, setRoute] = useState(readHashRoute)

  useEffect(() => {
    if (typeof window === 'undefined') {
      return undefined
    }

    function handleHashChange() {
      setRoute(readHashRoute())
    }

    window.addEventListener('hashchange', handleHashChange)

    return () => {
      window.removeEventListener('hashchange', handleHashChange)
    }
  }, [])

  const showDashboardRoute = useCallback(() => {
    clearRoute()
    setRoute({
      isUserPageOpen: false,
      selectedAnimeId: null,
    })
  }, [])

  const clearAnimeRoute = useCallback(() => {
    if (readSelectedAnimeId() !== null) {
      clearRoute()
      setRoute({
        isUserPageOpen: false,
        selectedAnimeId: null,
      })
      return
    }

    setRoute(readHashRoute())
  }, [])

  const openAnimeRoute = useCallback((animeId) => {
    if (typeof window === 'undefined') {
      return
    }

    window.location.hash = `/anime/${animeId}`
    setRoute({
      isUserPageOpen: false,
      selectedAnimeId: animeId,
    })
  }, [])

  const openUserRoute = useCallback(() => {
    if (typeof window === 'undefined') {
      return
    }

    window.location.hash = '/user'
    setRoute({
      isUserPageOpen: true,
      selectedAnimeId: null,
    })
  }, [])

  return {
    ...route,
    isDetailsOpen: !route.isUserPageOpen && route.selectedAnimeId !== null,
    clearAnimeRoute,
    openAnimeRoute,
    openUserRoute,
    showDashboardRoute,
  }
}
