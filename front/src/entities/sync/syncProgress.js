export function formatSyncProgressMessage(job) {
  if (!job) {
    return ''
  }

  if (job.status === 'completed') {
    return job.message || 'Sync completed.'
  }

  if (job.status === 'failed') {
    return job.error || job.message || 'Sync failed.'
  }

  return 'Loading anime list...'
}
