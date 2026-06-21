import { useEffect, useState } from 'react'
import { fetchFranchises } from '../../shared/api/api'

// Loads the catalog-wide list of franchise groups for the "All anime" page. The
// endpoint reads only the global catalog, so it works with or without a session.
// Fetching is gated by `active` so the request runs only while the page is open,
// and in-flight requests are cancelled on close or unmount.
export default function useFranchises(active) {
  const [franchises, setFranchises] = useState([])
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!active) {
      return undefined
    }

    const controller = new AbortController()
    setIsLoading(true)
    setError('')

    fetchFranchises({ signal: controller.signal })
      .then((response) => {
        setFranchises(Array.isArray(response) ? response : [])
        setIsLoading(false)
      })
      .catch((requestError) => {
        if (controller.signal.aborted || requestError.name === 'AbortError') {
          return
        }

        setFranchises([])
        setError(requestError.message)
        setIsLoading(false)
      })

    return () => {
      controller.abort()
    }
  }, [active])

  return { franchises, isLoading, error }
}
