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

export const titleCollator = new Intl.Collator('en', {
  sensitivity: 'base',
  numeric: true,
})
