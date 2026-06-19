import { useEffect, useState } from 'react'
import { fetchFranchise } from '../../shared/api/api'

// Loads a single franchise group by anime id from the public endpoint, so the
// franchise view works without a signed-in session. Fetching is keyed by the
// selected anime id and cancels in-flight requests on change or unmount.
export default function useFranchise(animeId) {
  const [franchise, setFranchise] = useState(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')

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
    setFranchise(null)

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
  }, [animeId])

  return { franchise, isLoading, error }
}
