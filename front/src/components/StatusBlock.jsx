function StatusBlock({ statusMessage, errorMessage }) {
  return (
    <div className="status-block">
      {/* Request state feedback */}
      <p className="status-message">{statusMessage}</p>
      <p className="hint">
        Your MAL token is stored on the backend; the browser only keeps a session cookie.
      </p>
      {errorMessage ? <p className="error-banner">{errorMessage}</p> : null}
    </div>
  )
}

export default StatusBlock
