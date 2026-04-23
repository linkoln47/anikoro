function progressPercent(progress) {
  if (!progress || progress.total <= 0) {
    return 0
  }

  return Math.max(
    0,
    Math.min(100, Math.round((progress.current / progress.total) * 100)),
  )
}

function StatusBlock({ statusMessage, errorMessage, mode, progress }) {
  const hint =
    mode === 'public'
      ? 'Public mode reads the latest synced snapshot for an open MAL list.'
      : mode === 'session'
        ? 'Signed-in mode uses your backend session cookie.'
        : 'Public search works for open MAL lists.'
  const percent = progressPercent(progress)
  const hasProgress = Boolean(progress)
  const progressLabel =
    progress && progress.total > 0
      ? `${progress.current}/${progress.total}`
      : progress?.status ?? ''

  return (
    <div className="status-block">
      {/* Request state feedback */}
      <p className="status-message">{statusMessage}</p>
      {hasProgress ? (
        <div className="sync-progress" aria-live="polite">
          <div className="sync-progress-header">
            <span>{progress.message}</span>
            <span>{progressLabel}</span>
          </div>
          <div
            className="sync-progress-track"
            role="progressbar"
            aria-label="Sync progress"
            aria-valuemin="0"
            aria-valuemax={progress.total > 0 ? progress.total : undefined}
            aria-valuenow={progress.total > 0 ? progress.current : undefined}
          >
            <span
              className={
                progress.total > 0
                  ? 'sync-progress-fill'
                  : 'sync-progress-fill is-indeterminate'
              }
              style={{ width: progress.total > 0 ? `${percent}%` : '38%' }}
            />
          </div>
        </div>
      ) : null}
      <p className="hint">{hint}</p>
      {errorMessage ? <p className="error-banner">{errorMessage}</p> : null}
    </div>
  )
}

export default StatusBlock
