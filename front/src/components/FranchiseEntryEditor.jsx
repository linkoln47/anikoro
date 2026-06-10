import { useEffect, useRef, useState } from 'react'

const EPISODES_COMMIT_DEBOUNCE_MS = 600

const statusOptions = [
  { value: 'watching', label: 'Watching' },
  { value: 'completed', label: 'Completed' },
  { value: 'on_hold', label: 'On hold' },
  { value: 'dropped', label: 'Dropped' },
  { value: 'plan_to_watch', label: 'Plan to watch' },
]

function clampEpisodes(value, maxEpisodes) {
  if (!Number.isInteger(value) || value < 0) {
    return 0
  }
  if (maxEpisodes > 0 && value > maxEpisodes) {
    return maxEpisodes
  }
  return value
}

function FranchiseEntryEditor({ item, isPending, onUpdateEntry }) {
  const commitTimerRef = useRef(null)
  const [draftEpisodes, setDraftEpisodes] = useState(null)
  const [episodesInput, setEpisodesInput] = useState(null)

  const maxEpisodes = item.num_episodes ?? 0
  const watchedEpisodes = draftEpisodes ?? item.watched_episodes ?? 0

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

  const handleStatusChange = (event) => {
    const status = event.target.value
    if (!status || status === item.user_list_status) {
      return
    }
    void onUpdateEntry(item.id, { status })
  }

  const handleScoreChange = (event) => {
    const score = Number.parseInt(event.target.value, 10)
    if (!Number.isInteger(score) || score === item.user_score) {
      return
    }
    void onUpdateEntry(item.id, { score })
  }

  if (!item.in_user_list) {
    return (
      <div className="franchise-edit">
        <label className="franchise-edit-field">
          <span className="field-label">Add to list</span>
          <select
            className="select-input franchise-edit-select"
            value=""
            disabled={isPending}
            onChange={handleStatusChange}
          >
            <option value="" disabled>
              Choose status...
            </option>
            {statusOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>
      </div>
    )
  }

  return (
    <div className="franchise-edit">
      <label className="franchise-edit-field">
        <span className="field-label">Status</span>
        <select
          className="select-input franchise-edit-select"
          value={item.user_list_status ?? ''}
          disabled={isPending}
          onChange={handleStatusChange}
        >
          {statusOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>

      <label className="franchise-edit-field">
        <span className="field-label">Score</span>
        <select
          className="select-input franchise-edit-select"
          value={item.user_score ?? 0}
          disabled={isPending}
          onChange={handleScoreChange}
        >
          <option value={0}>No score</option>
          {Array.from({ length: 10 }, (_, index) => index + 1).map((score) => (
            <option key={score} value={score}>
              {score}
            </option>
          ))}
        </select>
      </label>

      <div className="franchise-edit-field">
        <span className="field-label">
          Episodes{maxEpisodes > 0 ? ` / ${maxEpisodes}` : ''}
        </span>
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
      </div>
    </div>
  )
}

export default FranchiseEntryEditor
