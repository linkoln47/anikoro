import { useState } from 'react'
import {
  validateAccountUsername,
  validateEmail,
  validatePassword,
} from '../shared/security/inputValidation'

function AuthPanel({
  initialMode = 'login',
  onSubmit,
  onCancel,
  isSubmitting = false,
  serverError = '',
}) {
  const [mode, setMode] = useState(initialMode)
  const [email, setEmail] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [validationError, setValidationError] = useState('')

  const isRegister = mode === 'register'

  function switchMode(nextMode) {
    if (isSubmitting) {
      return
    }
    setMode(nextMode)
    setValidationError('')
  }

  function handleSubmit(event) {
    event.preventDefault()

    const emailResult = validateEmail(email)
    if (!emailResult.ok) {
      setValidationError(emailResult.error)
      return
    }

    let usernameValue
    if (isRegister) {
      const usernameResult = validateAccountUsername(username)
      if (!usernameResult.ok) {
        setValidationError(usernameResult.error)
        return
      }
      usernameValue = usernameResult.value
    }

    const passwordResult = validatePassword(password)
    if (!passwordResult.ok) {
      setValidationError(passwordResult.error)
      return
    }

    setValidationError('')
    onSubmit({
      mode,
      email: emailResult.value,
      username: usernameValue,
      password: passwordResult.value,
    })
  }

  const message = validationError || serverError

  return (
    <div className="auth-panel-overlay" role="presentation" onClick={onCancel}>
      <div
        className="auth-panel"
        role="dialog"
        aria-modal="true"
        aria-label={isRegister ? 'Create account' : 'Sign in'}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="auth-panel-tabs">
          <button
            type="button"
            className={`auth-tab${!isRegister ? ' is-active' : ''}`}
            onClick={() => switchMode('login')}
            disabled={isSubmitting}
          >
            Sign in
          </button>
          <button
            type="button"
            className={`auth-tab${isRegister ? ' is-active' : ''}`}
            onClick={() => switchMode('register')}
            disabled={isSubmitting}
          >
            Register
          </button>
        </div>

        <form className="auth-form" onSubmit={handleSubmit}>
          <label className="auth-field">
            <span className="field-label">Email</span>
            <input
              className="text-input"
              type="email"
              autoComplete="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              disabled={isSubmitting}
              required
            />
          </label>

          {isRegister ? (
            <label className="auth-field">
              <span className="field-label">Username</span>
              <input
                className="text-input"
                type="text"
                autoComplete="username"
                value={username}
                onChange={(event) => setUsername(event.target.value)}
                disabled={isSubmitting}
                required
              />
            </label>
          ) : null}

          <label className="auth-field">
            <span className="field-label">Password</span>
            <input
              className="text-input"
              type="password"
              autoComplete={isRegister ? 'new-password' : 'current-password'}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              disabled={isSubmitting}
              required
            />
          </label>

          {message ? (
            <p className="field-error" role="alert">
              {message}
            </p>
          ) : null}

          <div className="action-row auth-panel-actions">
            <button
              type="button"
              className="ghost-button"
              onClick={onCancel}
              disabled={isSubmitting}
            >
              Cancel
            </button>
            <button type="submit" className="primary-button" disabled={isSubmitting}>
              {isSubmitting
                ? 'Please wait...'
                : isRegister
                  ? 'Create account'
                  : 'Sign in'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

export default AuthPanel
