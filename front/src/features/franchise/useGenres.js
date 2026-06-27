import { useEffect, useState } from 'react'
import { fetchGenres } from '../../shared/api/api'

// Loads the catalog's genre universe once for the "All anime" genre filter. The
// franchise grid is paged server-side, so its filter cannot derive options from a
// single loaded page the way the fully-loaded seasonal view can; the list is
// catalog-wide and effectively static within a session, so it is fetched once on
// mount. A failure only leaves the filter empty (disabled); it never breaks the page.
export default function useGenres() {
  const [genres, setGenres] = useState([])

  useEffect(() => {
    const controller = new AbortController()

    fetchGenres({ signal: controller.signal })
      .then((response) => {
        setGenres(Array.isArray(response?.genres) ? response.genres : [])
      })
      .catch((error) => {
        if (controller.signal.aborted || error.name === 'AbortError') {
          return
        }
        setGenres([])
      })

    return () => controller.abort()
  }, [])

  return genres
}
