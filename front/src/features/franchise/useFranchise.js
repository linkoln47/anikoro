import { useCallback, useEffect, useState } from 'react'
import { fetchFranchise } from '../../shared/api/api'

// Loads a single franchise group by anime id from the franchise endpoint. The
// endpoint works with or without a session: a signed-in caller's list marks are
// included, otherwise the user-only fields come back zeroed. Fetching is keyed
// by the selected anime id (and a reload token) and cancels in-flight requests
// on change or unmount. `reload` lets callers refetch after editing list marks.
export default function useFranchise(animeId) {
  const [franchise, setFranchise] = useState(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')
  const [reloadToken, setReloadToken] = useState(0)

  const reload = useCallback(() => {
    setReloadToken((token) => token + 1)
  }, [])

  useEffect(() => {
    if (!animeId) {
      setFranchise(null)
      setIsLoading(false)
      setError('')
      return undefined
    }

    const controller = new AbortController()
    setIsLoading(true)
    setError('')

    fetchFranchise(animeId, { signal: controller.signal })
      .then((response) => {
        setFranchise(response)
        setIsLoading(false)
      })
      .catch((requestError) => {
        if (controller.signal.aborted || requestError.name === 'AbortError') {
          return
        }

        setFranchise(null)
        setError(requestError.message)
        setIsLoading(false)
      })

    return () => {
      controller.abort()
    }
  }, [animeId, reloadToken])

  return { franchise, isLoading, error, reload }
}
