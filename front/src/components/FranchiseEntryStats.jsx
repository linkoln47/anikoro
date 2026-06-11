import { useEffect, useRef, useState } from 'react'
import { formatScore } from '../entities/anime/animeFormatters'

const EPISODES_COMMIT_DEBOUNCE_MS = 600

function clampEpisodes(value, maxEpisodes) {
  if (!Number.isInteger(value) || value < 0) {
    return 0
  }
  if (maxEpisodes > 0 && value > maxEpisodes) {
    return maxEpisodes
  }
  return value
}

function FranchiseEntryStats({ item, canEdit, isPending, onUpdateEntry }) {
  const commitTimerRef = useRef(null)
  const [draftEpisodes, setDraftEpisodes] = useState(null)
  const [episodesInput, setEpisodesInput] = useState(null)

  const maxEpisodes = item.num_episodes ?? 0
  const watchedEpisodes = draftEpisodes ?? item.watched_episodes ?? 0
  const isEditable = Boolean(canEdit && item.in_user_list)

  const cancelScheduledCommit = () => {
    if (commitTimerRef.current) {
      window.clearTimeout(commitTimerRef.current)
      commitTimerRef.current = null
    }
  }

  useEffect(() => {
    return () => {
      cancelScheduledCommit()
    }
  }, [])

  useEffect(() => {
    // The server confirmed a new value: drop the local draft.
    setDraftEpisodes(null)
  }, [item.watched_episodes])

  const commitEpisodes = async (value) => {
    cancelScheduledCommit()
    const result = await onUpdateEntry(item.id, { num_watched_episodes: value })
    if (!result) {
      setDraftEpisodes(null)
    }
  }

  const scheduleEpisodesCommit = (value) => {
    setDraftEpisodes(value)
    cancelScheduledCommit()
    commitTimerRef.current = window.setTimeout(() => {
      commitTimerRef.current = null
      void commitEpisodes(value)
    }, EPISODES_COMMIT_DEBOUNCE_MS)
  }

  const handleEpisodesStep = (delta) => {
    const next = clampEpisodes(watchedEpisodes + delta, maxEpisodes)
    if (next === watchedEpisodes) {
      return
    }
    scheduleEpisodesCommit(next)
  }

  const handleEpisodesInputCommit = () => {
    const raw = episodesInput
    setEpisodesInput(null)
    if (raw === null || raw.trim() === '') {
      return
    }

    const parsed = Number.parseInt(raw, 10)
    if (!Number.isInteger(parsed)) {
      return
    }

    const next = clampEpisodes(parsed, maxEpisodes)
    if (next === (item.watched_episodes ?? 0) && draftEpisodes === null) {
      return
    }
    setDraftEpisodes(next)
    void commitEpisodes(next)
  }

  const handleScoreChange = (event) => {
    const score = Number.parseInt(event.target.value, 10)
    if (!Number.isInteger(score) || score === item.user_score) {
      return
    }
    void onUpdateEntry(item.id, { score })
  }

  return (
    <dl className="franchise-card-stats">
      <div>
        <dt>Score</dt>
        <dd>
          {isEditable ? (
            <select
              className="select-input franchise-stat-select"
              value={item.user_score ?? 0}
              disabled={isPending}
              aria-label="User score"
              onChange={handleScoreChange}
            >
              <option value={0}>No score</option>
              {Array.from({ length: 10 }, (_, index) => index + 1).map((score) => (
                <option key={score} value={score}>
                  {score}
                </option>
              ))}
            </select>
          ) : item.in_user_list ? (
            formatScore(item.user_score)
          ) : (
            '-'
          )}
        </dd>
      </div>
      <div>
        <dt>Watched</dt>
        <dd>
          {isEditable ? (
            <div className="franchise-episode-stepper">
              <button
                className="ghost-button franchise-episode-step"
                type="button"
                disabled={isPending || watchedEpisodes <= 0}
                aria-label="Decrease watched episodes"
                onClick={() => handleEpisodesStep(-1)}
              >
                -
              </button>
              {episodesInput !== null ? (
                <input
                  className="text-input franchise-episode-input"
                  type="number"
                  min={0}
                  max={maxEpisodes > 0 ? maxEpisodes : undefined}
                  value={episodesInput}
                  autoFocus
                  onChange={(event) => setEpisodesInput(event.target.value)}
                  onBlur={handleEpisodesInputCommit}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') {
                      handleEpisodesInputCommit()
                    }
                    if (event.key === 'Escape') {
                      setEpisodesInput(null)
                    }
                  }}
                />
              ) : (
                <button
                  className="ghost-button franchise-episode-value"
                  type="button"
                  disabled={isPending}
                  aria-label="Edit watched episodes"
                  onClick={() => setEpisodesInput(String(watchedEpisodes))}
                >
                  {watchedEpisodes}
                </button>
              )}
              <button
                className="ghost-button franchise-episode-step"
                type="button"
                disabled={
                  isPending || (maxEpisodes > 0 && watchedEpisodes >= maxEpisodes)
                }
                aria-label="Increase watched episodes"
                onClick={() => handleEpisodesStep(1)}
              >
                +
              </button>
            </div>
          ) : item.in_user_list ? (
            item.watched_episodes
          ) : (
            '-'
          )}
        </dd>
      </div>
    </dl>
  )
}

export default FranchiseEntryStats
