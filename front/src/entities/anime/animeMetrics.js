export function readNumericValue(value) {
  const numeric = Number(value)
  return Number.isNaN(numeric) ? 0 : numeric
}

export function readScore(value) {
  const numeric = Number(value)
  return Number.isFinite(numeric) && numeric > 0 ? numeric : null
}

export function hasScore(value) {
  return readScore(value) !== null
}

export function readScoreValue(value) {
  return readScore(value) ?? -1
}

export function readStartDateValue(value) {
  const timestamp = Date.parse(value)
  return Number.isNaN(timestamp) ? 0 : timestamp
}
