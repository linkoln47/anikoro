import { scoreBuckets } from '../anime/animeConstants'
import { readNumericValue, readScore } from '../anime/animeMetrics'

export const numberFormatter = new Intl.NumberFormat('en')

export function buildScoreBuckets(anime) {
  const buckets = scoreBuckets.map((score) => ({
    score,
    count: 0,
  }))

  anime.forEach((item) => {
    const score = readScore(item.avg_score)
    if (score === null) {
      return
    }

    const roundedScore = Math.min(10, Math.max(1, Math.round(score)))
    buckets[roundedScore - 1].count += 1
  })

  return buckets
}

export function calculateAverageScore(anime) {
  const ratedScores = anime
    .map((item) => readScore(item.avg_score))
    .filter((score) => score !== null)

  if (ratedScores.length === 0) {
    return 0
  }

  return ratedScores.reduce((total, score) => total + score, 0) / ratedScores.length
}

export function sumWatchedEpisodes(anime) {
  return anime.reduce(
    (total, item) => total + readNumericValue(item.watched_episodes_sum),
    0,
  )
}
