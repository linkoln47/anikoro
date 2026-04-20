function UserControls({
  userIdInput,
  onUserIdChange,
  onLoad,
  onSync,
  onRefresh,
  isLoading,
  isSyncing,
}) {
  return (
    <form className="controls" onSubmit={onLoad}>
      {/* User lookup form */}
      <label className="field">
        <span className="field-label">App user id</span>
        <input
          className="text-input"
          type="text"
          inputMode="numeric"
          placeholder="Example: 1"
          value={userIdInput}
          onChange={(event) => onUserIdChange(event.target.value)}
        />
      </label>

      {/* Dashboard actions */}
      <div className="action-row">
        <button className="primary-button" type="submit" disabled={isLoading}>
          {isLoading ? 'Loading...' : 'Load Data'}
        </button>
        <button
          className="secondary-button"
          type="button"
          onClick={onSync}
          disabled={isSyncing}
        >
          {isSyncing ? 'Starting...' : 'Start Sync'}
        </button>
        <button
          className="ghost-button"
          type="button"
          onClick={onRefresh}
          disabled={isLoading}
        >
          Refresh
        </button>
      </div>
    </form>
  )
}

export default UserControls
