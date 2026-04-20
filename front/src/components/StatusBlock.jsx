function StatusBlock({ statusMessage, errorMessage }) {
  return (
    <div className="status-block">
      {/* Request state feedback */}
      <p className="status-message">{statusMessage}</p>
      <p className="hint">
        Use the internal <code>users.id</code> from PostgreSQL, not the MAL
        username.
      </p>
      {errorMessage ? <p className="error-banner">{errorMessage}</p> : null}
    </div>
  )
}

export default StatusBlock
