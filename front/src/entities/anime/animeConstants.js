export const franchiseStatusPriority = [
  'watching',
  'dropped',
  'on_hold',
  'completed',
  'plan_to_watch',
]

export const franchiseStatusLabels = {
  all: 'Any status',
  watching: 'Watching',
  completed: 'Completed',
  on_hold: 'On hold',
  dropped: 'Dropped',
  plan_to_watch: 'Plan to watch',
}

export const scoreBuckets = Array.from({ length: 10 }, (_, index) => index + 1)

// Catalog browse controls shared by the seasonal grid and the "all franchises"
// grid. Sort keys mirror the backend FranchiseQuery sort whitelist.
export const CATALOG_SORT_OPTIONS = [
  { key: 'score', label: 'Score' },
  { key: 'title', label: 'Title' },
  { key: 'date', label: 'Air date' },
  { key: 'episodes', label: 'Episodes' },
]

// Media-type filter options; '' = all. Values mirror anime_catalog.media_type and
// the backend's franchise media-type allow-list.
export const MEDIA_TYPE_FILTERS = [
  { value: '', label: 'All' },
  { value: 'tv', label: 'TV' },
  { value: 'movie', label: 'Movie' },
  { value: 'ova', label: 'OVA' },
  { value: 'ona', label: 'ONA' },
  { value: 'special', label: 'Special' },
  { value: 'music', label: 'Music' },
]

export const titleCollator = new Intl.Collator('en', {
  sensitivity: 'base',
  numeric: true,
})
