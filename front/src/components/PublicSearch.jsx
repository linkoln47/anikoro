function PublicSearch({
  username,
  onUsernameChange,
  onSearch,
  onSync,
  isLoading,
  isSyncing,
}) {
  const trimmedUsername = username.trim()
  const isDisabled = trimmedUsername === ''
  const isBusy = isLoading || isSyncing

  function handleSubmit(event) {
    event.preventDefault()
    if (isDisabled) {
      return
    }

    onSearch(trimmedUsername)
  }

  function handleSyncClick() {
    if (isDisabled) {
      return
    }

    onSync(trimmedUsername)
  }

  return (
    <form className="public-search" onSubmit={handleSubmit}>
      <label className="public-search-field">
        <span className="field-label">Public MAL username</span>
        <input
          className="text-input public-search-input"
          type="search"
          value={username}
          onChange={(event) => onUsernameChange(event.target.value)}
          placeholder="MAL username"
          autoComplete="off"
          disabled={isBusy}
        />
      </label>

      <div className="public-search-actions">
        <button
          className="primary-button"
          type="submit"
          disabled={isDisabled || isBusy}
        >
          {isLoading ? 'Searching...' : 'Search'}
        </button>
        <button
          className="secondary-button"
          type="button"
          onClick={handleSyncClick}
          disabled={isDisabled || isBusy}
        >
          {isSyncing ? 'Starting...' : 'Sync public list'}
        </button>
      </div>
    </form>
  )
}

export default PublicSearch
