import { useCallback, useEffect, useRef, useState } from 'react'
import { fetchFranchises } from '../../shared/api/api'

const PAGE_SIZE = 48
const SEARCH_DEBOUNCE_MS = 300

// Loads the catalog-wide franchise grid for the "All anime" page one page at a
// time. Filtering by media type and title and paging all happen server-side, so
// the page never pulls the whole catalog at once: it fetches the first window and
// appends further windows as `loadMore` is called (driven by the virtualized
// grid scrolling near its end). Changing the media-type filter or search term
// resets the list back to the first page; the search term is debounced so typing
// does not fire a request per keystroke. In-flight requests are cancelled when
// the filters change or the component unmounts.
export default function useFranchises({ mediaType = '', search = '' } = {}) {
  const [items, setItems] = useState([])
  const [total, setTotal] = useState(0)
  const [isLoading, setIsLoading] = useState(false)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [error, setError] = useState('')

  // Debounce the search term so each keystroke does not trigger a fetch.
  const [debouncedSearch, setDebouncedSearch] = useState(search)
  useEffect(() => {
    const handle = window.setTimeout(() => setDebouncedSearch(search), SEARCH_DEBOUNCE_MS)
    return () => window.clearTimeout(handle)
  }, [search])

  // Mutable request state shared across the load callbacks. offsetRef tracks how
  // many rows have been requested for the current filter, totalRef mirrors the
  // server's match count so loadMore can stop, and controllerRef cancels the
  // in-flight request when the filter changes or the page unmounts.
  const offsetRef = useRef(0)
  const totalRef = useRef(0)
  const controllerRef = useRef(null)
  const loadingRef = useRef(false)

  const load = useCallback(
    (reset) => {
      // A reset (filter/search change) always proceeds and cancels whatever is in
      // flight; a loadMore is skipped while a request is running or once every
      // matching row has been loaded.
      if (!reset) {
        if (loadingRef.current) {
          return
        }
        if (totalRef.current > 0 && offsetRef.current >= totalRef.current) {
          return
        }
      }

      controllerRef.current?.abort()
      const controller = new AbortController()
      controllerRef.current = controller
      loadingRef.current = true

      const offset = reset ? 0 : offsetRef.current
      if (reset) {
        offsetRef.current = 0
        setIsLoading(true)
        setError('')
      } else {
        setIsLoadingMore(true)
      }

      fetchFranchises({
        mediaType,
        search: debouncedSearch,
        limit: PAGE_SIZE,
        offset,
        signal: controller.signal,
      })
        .then((response) => {
          const pageItems = Array.isArray(response?.items) ? response.items : []
          const pageTotal = Number.isInteger(response?.total) ? response.total : 0

          totalRef.current = pageTotal
          offsetRef.current = offset + pageItems.length
          loadingRef.current = false

          setTotal(pageTotal)
          setItems((prev) => (reset ? pageItems : prev.concat(pageItems)))
          setIsLoading(false)
          setIsLoadingMore(false)
        })
        .catch((requestError) => {
          if (controller.signal.aborted || requestError.name === 'AbortError') {
            return
          }

          loadingRef.current = false
          setError(requestError.message)
          setIsLoading(false)
          setIsLoadingMore(false)
        })
    },
    [mediaType, debouncedSearch],
  )

  // Reset to the first page whenever the active filter or debounced search
  // changes (and on mount); cancel the in-flight request on unmount or before the
  // next reset.
  useEffect(() => {
    load(true)
    return () => {
      controllerRef.current?.abort()
    }
  }, [load])

  const loadMore = useCallback(() => {
    load(false)
  }, [load])

  return {
    items,
    total,
    isLoading,
    isLoadingMore,
    error,
    hasMore: items.length < total,
    loadMore,
  }
}
