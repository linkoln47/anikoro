import { useEffect, useState } from 'react'
import { validateAccountUsername } from '../shared/security/inputValidation'

function PublicSearch({
  username,
  onUsernameChange,
  onSearch,
  isLoading,
}) {
  const [draftUsername, setDraftUsername] = useState(username)
  const usernameValidation = validateAccountUsername(draftUsername)
  const hasDraftUsername = draftUsername.trim() !== ''
  const validationError = hasDraftUsername && !usernameValidation.ok
    ? usernameValidation.error
    : ''
  const isDisabled = !usernameValidation.ok

  useEffect(() => {
    setDraftUsername(username)
  }, [username])

  function handleUsernameChange(event) {
    const nextDraftUsername = event.target.value
    const nextValidation = validateAccountUsername(nextDraftUsername)

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

  return (
    <form className="public-search" onSubmit={handleSubmit}>
      <label className="public-search-field">
        <span className="field-label">anikoro username</span>
        <input
          className="text-input public-search-input"
          type="search"
          value={draftUsername}
          onChange={handleUsernameChange}
          placeholder="Native account username"
          autoComplete="off"
          disabled={isLoading}
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
          disabled={isDisabled || isLoading}
        >
          {isLoading ? 'Searching...' : 'Search'}
        </button>
      </div>
    </form>
  )
}

export default PublicSearch
