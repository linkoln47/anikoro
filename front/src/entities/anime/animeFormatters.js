const mediaTypeLabels = {
  tv: 'TV',
  movie: 'Movie',
  ova: 'OVA',
  ona: 'ONA',
  special: 'Special',
  music: 'Music',
}

function parseDisplayDate(value) {
  const dateOnlyMatch = String(value).match(/^(\d{4})-(\d{2})-(\d{2})$/)
  if (dateOnlyMatch) {
    const [, year, month, day] = dateOnlyMatch
    return {
      date: new Date(Date.UTC(Number(year), Number(month) - 1, Number(day))),
      isDateOnly: true,
    }
  }

  return {
    date: new Date(value),
    isDateOnly: false,
  }
}

function formatDate(value, options, fallback) {
  if (!value) {
    return fallback
  }

  const { date, isDateOnly } = parseDisplayDate(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }

  return new Intl.DateTimeFormat('en', {
    ...options,
    ...(isDateOnly ? { timeZone: 'UTC' } : {}),
  }).format(date)
}

export function formatAirStart(value) {
  return formatDate(value, { dateStyle: 'medium' }, 'n/a')
}

export function formatStartDate(value) {
  return formatDate(value, { dateStyle: 'medium' }, 'Unknown start')
}

export function formatSyncedAt(value) {
  return formatDate(value, { dateStyle: 'medium', timeStyle: 'short' }, 'n/a')
}

export function formatScore(value) {
  const numeric = Number(value)
  if (Number.isNaN(numeric) || numeric <= 0) {
    return '-'
  }

  return Number.isInteger(numeric) ? numeric.toFixed(0) : numeric.toFixed(1)
}

export function formatTypeLabel(value) {
  if (value === 'series') {
    return 'Series'
  }

  if (value === 'movie') {
    return 'Movie'
  }

  return value
}

export function formatMediaType(value) {
  if (!value) {
    return 'Unknown type'
  }

  return mediaTypeLabels[value] ?? value.replace(/_/g, ' ')
}
