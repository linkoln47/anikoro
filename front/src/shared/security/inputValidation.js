export const MAL_USERNAME_MIN_LENGTH = 2
export const MAL_USERNAME_MAX_LENGTH = 32
export const SYNC_JOB_ID_LENGTH = 24

const CONTROL_CHARACTER_PATTERN = /[\u0000-\u001F\u007F]/u
const MAL_USERNAME_PATTERN = /^[A-Za-z0-9_-]+$/u
const SYNC_JOB_ID_PATTERN = /^[A-Za-z0-9_-]+$/u

function normalizeInputValue(value) {
  return String(value ?? '').normalize('NFKC').trim()
}

function normalizeOpaqueValue(value) {
  return String(value ?? '').trim()
}

export function validateMalUsername(value) {
  const username = normalizeInputValue(value)

  if (username === '') {
    return {
      ok: false,
      value: '',
      error: 'Enter a MAL username.',
    }
  }

  if (CONTROL_CHARACTER_PATTERN.test(username)) {
    return {
      ok: false,
      value: '',
      error: 'MAL username contains unsupported characters.',
    }
  }

  if (username.length < MAL_USERNAME_MIN_LENGTH) {
    return {
      ok: false,
      value: '',
      error: `MAL username must be at least ${MAL_USERNAME_MIN_LENGTH} characters.`,
    }
  }

  if (username.length > MAL_USERNAME_MAX_LENGTH) {
    return {
      ok: false,
      value: '',
      error: `MAL username must be ${MAL_USERNAME_MAX_LENGTH} characters or fewer.`,
    }
  }

  if (!MAL_USERNAME_PATTERN.test(username)) {
    return {
      ok: false,
      value: '',
      error: 'Use only letters, numbers, underscores, or hyphens.',
    }
  }

  return {
    ok: true,
    value: username,
    error: '',
  }
}

export function parseMalUsername(value) {
  const result = validateMalUsername(value)

  if (!result.ok) {
    throw new Error(result.error)
  }

  return result.value
}

export function validateSyncJobId(value) {
  const jobId = normalizeOpaqueValue(value)

  if (jobId === '') {
    return {
      ok: false,
      value: '',
      error: 'Sync job id is required.',
    }
  }

  if (CONTROL_CHARACTER_PATTERN.test(jobId)) {
    return {
      ok: false,
      value: '',
      error: 'Sync job id contains unsupported characters.',
    }
  }

  if (jobId.length !== SYNC_JOB_ID_LENGTH) {
    return {
      ok: false,
      value: '',
      error: `Sync job id must be exactly ${SYNC_JOB_ID_LENGTH} characters.`,
    }
  }

  if (!SYNC_JOB_ID_PATTERN.test(jobId)) {
    return {
      ok: false,
      value: '',
      error: 'Sync job id has an invalid format.',
    }
  }

  return {
    ok: true,
    value: jobId,
    error: '',
  }
}

export function parseSyncJobId(value) {
  const result = validateSyncJobId(value)

  if (!result.ok) {
    throw new Error(result.error)
  }

  return result.value
}
