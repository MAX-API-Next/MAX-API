// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { invalidateModelPricing } from '@/features/system-settings/models/model-pricing-queries'
import {
  PluginError,
  PluginRequestOrder,
  type PluginMutation,
  type PluginSnapshot,
  type TaskPluginAPI,
} from './api'

// The query cache manages transport; accepted observations and confirmation
// guards remain explicit. Neither a refetch nor a matching state certifies an
// unknown write. No automatic retry, focus refresh or optimistic catalog edit.
export function useTaskPluginCatalog(transport: TaskPluginAPI) {
  const queryClient = useQueryClient()
  const instance = useId()
  const queryKey = ['task-plugin-catalog', instance, transport] as const
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => transport.read(signal),
    enabled: false,
    retry: false,
    gcTime: 0,
    refetchOnMount: false,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
  })
  const mutation = useMutation({
    mutationFn: (command: PluginMutation) => transport.mutate(command),
    retry: false,
    gcTime: 0,
  })
  const refetch = query.refetch
  const mutateAsync = mutation.mutateAsync
  const [snapshot, setSnapshot] = useState<PluginSnapshot>()
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [readError, setReadError] = useState('')
  const [notice, setNotice] = useState('')
  const [unknown, setUnknown] = useState(false)
  const [readAfterUnknown, setReadAfterUnknown] = useState(false)
  const reads = useRef(new PluginRequestOrder())
  const unknownRef = useRef(false)
  const mounted = useRef(true)
  const writing = useRef(false)
  const acceptObservation = useCallback(
    (
      id: number,
      result: { isError: boolean; data: PluginSnapshot | undefined }
    ) => {
      if (!mounted.current || !reads.current.current(id)) return
      if (result.isError || !result.data) {
        setSnapshot(undefined)
        setReadError('Task plugin catalog could not be loaded')
        setReadAfterUnknown(false)
      } else {
        setSnapshot(result.data)
        setReadError('')
        setReadAfterUnknown(unknownRef.current)
      }
      setLoading(false)
    },
    []
  )
  const readSnapshot = useCallback(async () => {
    const id = reads.current.next()
    try {
      // Cancel even an initial in-flight query before refetching: otherwise
      // deduplication could reuse a read started before the write.
      await queryClient.cancelQueries({
        queryKey: ['task-plugin-catalog', instance],
      })
      if (!mounted.current || !reads.current.current(id)) return
      acceptObservation(id, await refetch({ cancelRefetch: true }))
    } catch {
      acceptObservation(id, { isError: true, data: undefined })
    }
  }, [acceptObservation, instance, queryClient, refetch])
  const refresh = useCallback(() => {
    setLoading(true)
    return readSnapshot()
  }, [readSnapshot])
  useEffect(() => {
    mounted.current = true
    const order = reads.current
    const id = order.next()
    // Apply state only from the actual asynchronous query completion.
    void refetch({ cancelRefetch: true }).then(
      (result) => acceptObservation(id, result),
      () => acceptObservation(id, { isError: true, data: undefined })
    )
    return () => {
      mounted.current = false
      order.next()
      void queryClient.cancelQueries({
        queryKey: ['task-plugin-catalog', instance],
      })
    }
  }, [acceptObservation, instance, queryClient, refetch, transport])

  async function mutate(command: PluginMutation, onAccepted?: () => void) {
    if (writing.current || unknownRef.current || !snapshot) return
    writing.current = true
    reads.current.next()
    setBusy(true)
    setError('')
    setNotice('')
    try {
      await mutateAsync(command)
      if (!mounted.current) return
      setNotice(
        'Plugin catalog updated. Execution and paid admission remain unavailable.'
      )
      onAccepted?.()
    } catch (e) {
      if (!mounted.current) return
      if (!(e instanceof PluginError) || e.kind === 'unknown') {
        unknownRef.current = true
        setUnknown(true)
        setReadAfterUnknown(false)
      }
      setError(
        e instanceof PluginError ? e.message : 'Task plugin request failed'
      )
    } finally {
      if (mounted.current) {
        invalidateModelPricing(queryClient)
        await refresh()
        writing.current = false
        setBusy(false)
      }
    }
  }
  function continueFromObservation() {
    if (!readAfterUnknown || busy || loading) return
    unknownRef.current = false
    setUnknown(false)
    setNotice(
      'Continue using the observed catalog revision; the previous result is not certified.'
    )
  }
  return {
    snapshot,
    loading,
    busy,
    error,
    setError,
    readError,
    notice,
    unknown,
    readAfterUnknown,
    refresh,
    mutate,
    continueFromObservation,
  }
}
