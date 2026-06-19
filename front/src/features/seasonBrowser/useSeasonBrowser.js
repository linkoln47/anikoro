import { useEffect, useState } from 'react'
import { fetchSeasonAnime } from '../../shared/api/api'

// Loads the anime stored for a season straight from the backend (which reads
// only the local catalog). It refetches whenever the selected season changes
// and cancels in-flight requests when the season changes again or the view
// unmounts.
export default function useSeasonBrowser(season) {
  const year = season?.year ?? null
  const name = season?.season ?? null
  const [anime, setAnime] = useState([])
  const [isLoading, setIsLoading] = useState(Boolean(year && name))
  const [error, setError] = useState('')

  useEffect(() => {
    if (!year || !name) {
      setAnime([])
      setIsLoading(false)
      setError('')
      return undefined
    }

    const controller = new AbortController()
    setIsLoading(true)
    setError('')

    fetchSeasonAnime(year, name, { signal: controller.signal })
      .then((response) => {
        setAnime(Array.isArray(response?.anime) ? response.anime : [])
        setIsLoading(false)
      })
      .catch((requestError) => {
        if (controller.signal.aborted || requestError.name === 'AbortError') {
          return
        }

        setAnime([])
        setError(requestError.message)
        setIsLoading(false)
      })

    return () => {
      controller.abort()
    }
  }, [year, name])

  return { anime, isLoading, error }
}
