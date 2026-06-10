import { useCallback, useState } from 'react'
import { updateAnimeListStatus } from '../../shared/api/malApi'

export default function useListEdit({ onEntryUpdated, onErrorMessage }) {
  const [pendingAnimeIds, setPendingAnimeIds] = useState(() => new Set())

  const updateListEntry = useCallback(async (animeId, patch) => {
    setPendingAnimeIds((current) => {
      const next = new Set(current)
      next.add(animeId)
      return next
    })

    try {
      const entry = await updateAnimeListStatus(animeId, patch)
      onEntryUpdated(entry)
      return entry
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
  }, [onEntryUpdated, onErrorMessage])

  return {
    pendingAnimeIds,
    updateListEntry,
  }
}
