import { useCallback, useState } from 'react'
import { removeAnimeListStatus, updateAnimeListStatus } from '../../shared/api/api'

export default function useListEdit({ onEntryUpdated, onEntryRemoved, onErrorMessage }) {
  const [pendingAnimeIds, setPendingAnimeIds] = useState(() => new Set())

  const withPendingAnime = useCallback(async (animeId, action) => {
    setPendingAnimeIds((current) => {
      const next = new Set(current)
      next.add(animeId)
      return next
    })

    try {
      return await action()
    } catch (error) {
      onErrorMessage(error.message)
      return null
    } finally {
      setPendingAnimeIds((current) => {
        const next = new Set(current)
        next.delete(animeId)
        return next
      })
    }
  }, [onErrorMessage])

  const updateListEntry = useCallback((animeId, patch) => {
    return withPendingAnime(animeId, async () => {
      const entry = await updateAnimeListStatus(animeId, patch)
      onEntryUpdated(entry)
      return entry
    })
  }, [withPendingAnime, onEntryUpdated])

  const removeListEntry = useCallback((animeId) => {
    return withPendingAnime(animeId, async () => {
      const result = await removeAnimeListStatus(animeId)
      onEntryRemoved(result.anime_id)
      return result
    })
  }, [withPendingAnime, onEntryRemoved])

  return {
    pendingAnimeIds,
    removeListEntry,
    updateListEntry,
  }
}
