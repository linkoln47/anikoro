import { titleCollator } from './animeConstants'
import { readNumericValue, readScoreValue, readStartDateValue } from './animeMetrics'
import { getAirStart } from './animeSelectors'

function compareAnime(left, right, key) {
  switch (key) {
    case 'id':
      return left.id - right.id
    case 'title':
      return titleCollator.compare(left.display_title, right.display_title)
    case 'score':
      return readScoreValue(left.avg_score) - readScoreValue(right.avg_score)
    case 'merged':
      return readNumericValue(left.merged_titles) - readNumericValue(right.merged_titles)
    case 'watched':
      return (
        readNumericValue(left.watched_episodes_sum) -
        readNumericValue(right.watched_episodes_sum)
      )
    case 'airStart':
      return readStartDateValue(getAirStart(left)) - readStartDateValue(getAirStart(right))
    default:
      return 0
  }
}

export function sortAnime(items, sortState) {
  const sorted = [...items].sort((left, right) => left.id - right.id)

  if (!sortState.key || !sortState.direction) {
    return sorted
  }

  const directionMultiplier = sortState.direction === 'asc' ? 1 : -1

  return sorted.sort((left, right) => {
    const primaryCompare = compareAnime(left, right, sortState.key)
    if (primaryCompare !== 0) {
      return primaryCompare * directionMultiplier
    }

    return left.id - right.id
  })
}
