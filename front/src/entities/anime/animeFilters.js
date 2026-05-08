import { hasScore } from './animeMetrics'
import { getFranchiseStatus } from './animeSelectors'

export function filterAnime(items, filters) {
  const {
    searchQuery = '',
    scoreFilter = 'all',
    statusFilter = 'all',
    typeFilter = 'all',
  } = filters
  const normalizedQuery = searchQuery.trim().toLowerCase()

  return items.filter((item) => {
    if (typeFilter !== 'all' && item.type !== typeFilter) {
      return false
    }

    if (scoreFilter === 'scored' && !hasScore(item.avg_score)) {
      return false
    }

    if (scoreFilter === 'unscored' && hasScore(item.avg_score)) {
      return false
    }

    if (statusFilter !== 'all' && getFranchiseStatus(item) !== statusFilter) {
      return false
    }

    if (!normalizedQuery) {
      return true
    }

    return item.display_title.toLowerCase().includes(normalizedQuery)
  })
}

export function hasActiveAnimeFilters(filters) {
  return (
    filters.typeFilter !== 'all' ||
    filters.scoreFilter !== 'all' ||
    filters.statusFilter !== 'all'
  )
}

export function countActiveAnimeFilters(filters) {
  return (
    Number(filters.typeFilter !== 'all') +
    Number(filters.scoreFilter !== 'all') +
    Number(filters.statusFilter !== 'all')
  )
}
