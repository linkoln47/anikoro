import { useEffect, useState } from 'react'
import { validateMalUsername } from '../shared/security/inputValidation'

function PublicSearch({
  username,
  onUsernameChange,
  onSearch,
  onSync,
  isLoading,
  isSyncing,
  syncCooldownSeconds = 0,
}) {
  const [draftUsername, setDraftUsername] = useState(username)
  const usernameValidation = validateMalUsername(draftUsername)
  const hasDraftUsername = draftUsername.trim() !== ''
  const validationError = hasDraftUsername && !usernameValidation.ok
    ? usernameValidation.error
    : ''
  const isDisabled = !usernameValidation.ok
  const isBusy = isLoading || isSyncing
  const isSyncCooldownActive = syncCooldownSeconds > 0

  useEffect(() => {
    setDraftUsername(username)
  }, [username])

  function handleUsernameChange(event) {
    const nextDraftUsername = event.target.value
    const nextValidation = validateMalUsername(nextDraftUsername)

    setDraftUsername(nextDraftUsername)

    if (nextValidation.ok) {
      onUsernameChange(nextValidation.value)
    } else if (nextDraftUsername.trim() === '') {
      onUsernameChange('')
    }
  }

  function handleSubmit(event) {
    event.preventDefault()
    if (!usernameValidation.ok) {
      return
    }

    onSearch(usernameValidation.value)
  }

  function handleSyncClick() {
    if (!usernameValidation.ok) {
      return
    }

    onSync(usernameValidation.value)
  }

  return (
    <form className="public-search" onSubmit={handleSubmit}>
      <label className="public-search-field">
        <span className="field-label">Public MAL username</span>
        <input
          className="text-input public-search-input"
          type="search"
          value={draftUsername}
          onChange={handleUsernameChange}
          placeholder="MAL username"
          autoComplete="off"
          disabled={isBusy}
          aria-invalid={validationError ? 'true' : 'false'}
          aria-describedby={validationError ? 'public-search-error' : undefined}
        />
        {validationError ? (
          <p className="field-error" id="public-search-error">
            {validationError}
          </p>
        ) : null}
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
          disabled={isDisabled || isBusy || isSyncCooldownActive}
        >
          {isSyncing
            ? 'Starting...'
            : isSyncCooldownActive
              ? `Wait ${syncCooldownSeconds}s`
              : 'Sync public list'}
        </button>
      </div>
    </form>
  )
}

export default PublicSearch
