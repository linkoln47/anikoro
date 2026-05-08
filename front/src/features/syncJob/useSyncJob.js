import { useCallback, useEffect, useRef, useState } from 'react'
import { formatSyncProgressMessage } from '../../entities/sync/syncProgress'
import { fetchSyncJob, syncJobEventsUrl } from '../../shared/api/malApi'

function parseSyncJobPayload(payload) {
  try {
    return JSON.parse(payload)
  } catch {
    throw new Error('Received invalid sync progress update.')
  }
}

export default function useSyncJob({
  onErrorMessage,
  onPublicCompleted,
  onSessionCompleted,
  onStatusMessage,
}) {
  const sourceRef = useRef(null)
  const activeContextRef = useRef(null)
  const activeJobIdRef = useRef(null)
  const [activeContext, setActiveContext] = useState(null)
  const [syncProgress, setSyncProgress] = useState(null)

  const closeSyncEvents = useCallback(() => {
    sourceRef.current?.close()
    sourceRef.current = null
  }, [])

  const clearActiveSync = useCallback((context) => {
    if (context && activeContextRef.current !== context) {
      return
    }

    activeContextRef.current = null
    activeJobIdRef.current = null
    setActiveContext(null)
  }, [])

  const clearSyncProgress = useCallback(() => {
    closeSyncEvents()
    clearActiveSync()
    setSyncProgress(null)
  }, [clearActiveSync, closeSyncEvents])

  const endSync = useCallback((context) => {
    if (context && activeContextRef.current !== context) {
      return
    }

    closeSyncEvents()
    clearActiveSync(context)
  }, [clearActiveSync, closeSyncEvents])

  const clearFinishedProgress = useCallback(() => {
    if (!activeContextRef.current) {
      setSyncProgress(null)
    }
  }, [])

  const beginSync = useCallback((context) => {
    closeSyncEvents()
    activeContextRef.current = context
    activeJobIdRef.current = null
    setActiveContext(context)
    setSyncProgress(null)
  }, [closeSyncEvents])

  const finishSyncJob = useCallback((context, job) => {
    if (activeContextRef.current !== context) {
      return
    }

    closeSyncEvents()
    setSyncProgress(job)
    onStatusMessage(formatSyncProgressMessage(job))
    clearActiveSync(context)

    if (job.status === 'completed') {
      if (context.mode === 'public') {
        onPublicCompleted(context, job)
        return
      }

      onSessionCompleted(context, job)
      return
    }

    if (job.status === 'failed') {
      onErrorMessage(job.error || job.message || 'Sync failed.')
    }
  }, [
    clearActiveSync,
    closeSyncEvents,
    onErrorMessage,
    onPublicCompleted,
    onSessionCompleted,
    onStatusMessage,
  ])

  const watchSyncJob = useCallback((jobId, context) => {
    if (!jobId) {
      clearActiveSync(context)
      return
    }

    closeSyncEvents()
    activeContextRef.current = context
    activeJobIdRef.current = jobId
    setActiveContext(context)

    const source = new EventSource(syncJobEventsUrl(jobId), {
      withCredentials: true,
    })
    sourceRef.current = source

    source.onmessage = (event) => {
      if (activeContextRef.current !== context || activeJobIdRef.current !== jobId) {
        return
      }

      let job
      try {
        job = parseSyncJobPayload(event.data)
      } catch (error) {
        source.close()
        if (sourceRef.current === source) {
          sourceRef.current = null
        }
        clearActiveSync(context)
        onErrorMessage(error.message)
        onStatusMessage('Lost connection to sync progress.')
        return
      }

      setSyncProgress(job)
      onStatusMessage(formatSyncProgressMessage(job))

      if (job.status === 'completed' || job.status === 'failed') {
        finishSyncJob(context, job)
      }
    }

    source.onerror = () => {
      source.close()
      if (sourceRef.current === source) {
        sourceRef.current = null
      }

      if (activeContextRef.current !== context || activeJobIdRef.current !== jobId) {
        return
      }

      void fetchSyncJob(jobId)
        .then((job) => {
          if (activeContextRef.current !== context || activeJobIdRef.current !== jobId) {
            return
          }

          setSyncProgress(job)
          onStatusMessage(formatSyncProgressMessage(job))
          if (job.status === 'completed' || job.status === 'failed') {
            finishSyncJob(context, job)
            return
          }

          clearActiveSync(context)
          onErrorMessage('Lost connection to sync progress. Refresh the list in a few seconds.')
        })
        .catch((error) => {
          if (activeContextRef.current !== context || activeJobIdRef.current !== jobId) {
            return
          }

          clearActiveSync(context)
          onErrorMessage(error.message)
          onStatusMessage('Lost connection to sync progress.')
        })
    }
  }, [
    clearActiveSync,
    closeSyncEvents,
    finishSyncJob,
    onErrorMessage,
    onStatusMessage,
  ])

  useEffect(() => {
    return () => {
      closeSyncEvents()
    }
  }, [closeSyncEvents])

  return {
    beginSync,
    clearFinishedProgress,
    clearSyncProgress,
    endSync,
    isPublicSyncing: activeContext?.mode === 'public',
    isSessionSyncing: activeContext?.mode === 'session',
    syncProgress,
    watchSyncJob,
  }
}
