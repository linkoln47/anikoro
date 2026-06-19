import { franchiseStatusPriority, titleCollator } from './animeConstants'
import { readNumericValue, readStartDateValue } from './animeMetrics'

export function hasAnimeImage(item) {
  return Boolean(item?.image_medium_url || item?.image_large_url)
}

export function hasUserWatchedItem(item) {
  return item?.in_user_list && readNumericValue(item.watched_episodes) > 0
}

export function getFranchiseStatus(item) {
  const statusCounts = item.status_counts ?? {}

  for (const status of franchiseStatusPriority) {
    if (readNumericValue(statusCounts[status]) > 0) {
      return status
    }
  }

  const franchiseItems = Array.isArray(item.franchise) ? item.franchise : []

  for (const status of franchiseStatusPriority) {
    if (
      franchiseItems.some(
        (franchiseItem) =>
          franchiseItem.in_user_list && franchiseItem.user_list_status === status,
      )
    ) {
      return status
    }
  }

  return null
}

export function getPrimaryAnimeImage(item) {
  const franchiseItems = Array.isArray(item.franchise) ? item.franchise : []
  const earliestWatchedItem = franchiseItems
    .filter((franchiseItem) => hasUserWatchedItem(franchiseItem) && hasAnimeImage(franchiseItem))
    .sort((left, right) => readNumericValue(left.id) - readNumericValue(right.id))[0]
  const primaryItem =
    earliestWatchedItem ??
    franchiseItems.find((franchiseItem) => franchiseItem.id === item.id) ??
    franchiseItems[0]

  return primaryItem?.image_medium_url || primaryItem?.image_large_url || ''
}

// Season cards carry an individual MAL anime id, but the franchise detail view
// is keyed by the grouped representative id. Map a member id to the id of the
// group whose franchise contains it, so opening it resolves the right franchise.
export function findFranchiseGroupIdByMemberId(animeList, memberId) {
  if (!Array.isArray(animeList) || !memberId) {
    return null
  }

  for (const group of animeList) {
    if (group.id === memberId) {
      return group.id
    }

    const members = Array.isArray(group.franchise) ? group.franchise : []
    if (members.some((member) => member.id === memberId)) {
      return group.id
    }
  }

  return null
}

export function getPrimaryFranchiseItem(franchiseItems, selectedAnimeId) {
  const earliestWatchedItem = franchiseItems
    .filter((item) => hasUserWatchedItem(item) && hasAnimeImage(item))
    .sort((left, right) => readNumericValue(left.id) - readNumericValue(right.id))[0]

  return (
    earliestWatchedItem ??
    franchiseItems.find((item) => item.id === selectedAnimeId) ??
    franchiseItems[0] ??
    null
  )
}

export function getAirStart(item) {
  const franchiseItems = Array.isArray(item.franchise) ? item.franchise : []
  const datedItems = franchiseItems.filter((franchiseItem) => franchiseItem.start_date)

  if (datedItems.length === 0) {
    return ''
  }

  const seasonCandidates =
    item.type === 'series'
      ? datedItems.filter((franchiseItem) => franchiseItem.media_type === 'tv')
      : []
  const candidates = seasonCandidates.length > 0 ? seasonCandidates : datedItems

  return [...candidates]
    .sort((left, right) => {
      const dateCompare =
        readStartDateValue(left.start_date) - readStartDateValue(right.start_date)
      if (dateCompare !== 0) {
        return dateCompare
      }

      return titleCollator.compare(left.title ?? '', right.title ?? '')
    })[0]
    ?.start_date
}
