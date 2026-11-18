// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Selective workflow reference: QuantumNous/new-api 70e6af64 plugin list,
// upload and detail sheet. No upstream JS execution/editor/marketplace code.
import { useCallback, useEffect, useId, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, RefreshCw, Info } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { SectionPageLayout } from '@/components/layout'
import {
  taskPluginAPI,
  PluginError,
  PluginRequestOrder,
  type TaskPluginAPI,
  type PluginEntry,
  type PluginMutation,
} from './api'
import { readPluginFile } from './file-reader'
import { PluginCatalog } from './plugin-catalog'
import {
  PluginDetailSheet,
  type PluginConfirmation,
} from './plugin-detail-sheet'
import { PluginInstallSheet } from './plugin-install-sheet'
import { TaskPluginFeedback } from './task-plugin-feedback'
import { useTaskPluginCatalog } from './use-task-plugin-catalog'

const PACKAGE_LIMIT = 1024 * 1024
export function TaskPlugins(props: { transport?: TaskPluginAPI } = {}) {
  const transport = props.transport ?? taskPluginAPI
  const { t } = useTranslation()
  const bundleScope = useId()
  const queryClient = useQueryClient()
  const catalog = useTaskPluginCatalog(transport)
  const {
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
  } = catalog
  const [selected, setSelected] = useState<string>()
  const [installOpen, setInstallOpen] = useState(false)
  const [installRevision, setInstallRevision] = useState('')
  const [document, setDocument] = useState('')
  const [fileName, setFileName] = useState('')
  const [expectedHash, setExpectedHash] = useState('')
  const [bundledHash, setBundledHash] = useState<string>()
  const [validated, setValidated] = useState<PluginEntry>()
  const [validating, setValidating] = useState(false)
  const [nodesReady, setNodesReady] = useState(false)
  const bundleList = useQuery({
    queryKey: ['task-plugin-bundles', bundleScope, transport],
    queryFn: () => transport.bundled?.() ?? Promise.resolve([]),
    enabled: false,
    retry: false,
    gcTime: 0,
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
  })
  const bundles = bundleList.isError ? [] : (bundleList.data ?? [])
  const refetchBundles = bundleList.refetch
  const [bundleLoading, setBundleLoading] = useState(false)
  const [confirm, setConfirm] = useState<PluginConfirmation>()
  const validation = useRef(new PluginRequestOrder())
  const files = useRef(new PluginRequestOrder())
  const mounted = useRef(true)
  const catalogSearch = useRef<HTMLInputElement>(null)
  const onSearchReady = useCallback((element: HTMLInputElement | null) => {
    catalogSearch.current = element
  }, [])
  const resetValidation = () => {
    validation.current.next()
    setValidated(undefined)
    setValidating(false)
  }
  useEffect(() => {
    if (installOpen && transport.bundled)
      void refetchBundles({ cancelRefetch: true })
  }, [installOpen, transport, refetchBundles])

  useEffect(() => {
    mounted.current = true
    const validationOrder = validation.current
    const fileOrder = files.current
    return () => {
      mounted.current = false
      validationOrder.next()
      fileOrder.next()
    }
  }, [])

  useEffect(() => {
    if (installOpen && bundleList.isError)
      setError('Official plugin list could not be read')
  }, [installOpen, bundleList.isError, bundleList.errorUpdatedAt, setError])

  async function loadBundled(key: string, version: string) {
    const download = transport.bundle
    if (!download) return
    resetValidation()
    const id = files.current.next()
    setBundleLoading(true)
    setDocument('')
    setBundledHash(undefined)
    setExpectedHash('')
    setError('')
    try {
      const upload = await queryClient.fetchQuery({
        queryKey: ['task-plugin-source', bundleScope, transport, key, version],
        queryFn: () => download.call(transport, key, version),
        retry: false,
        gcTime: 0,
      })
      if (mounted.current && files.current.current(id)) {
        setDocument(upload.document)
        setExpectedHash(upload.expected_sha256)
        setBundledHash(upload.expected_sha256)
        setFileName(`${key}@${version}/plugin.js`)
        setNodesReady(false)
      }
    } catch (e) {
      if (mounted.current && files.current.current(id))
        setError(
          e instanceof PluginError
            ? e.message
            : 'Official plugin source could not be read'
        )
    } finally {
      if (mounted.current && files.current.current(id)) setBundleLoading(false)
    }
  }

  async function loadFile(file: File | undefined) {
    if (bundledHash !== undefined) setExpectedHash('')
    setBundledHash(undefined)
    setBundleLoading(false)
    setNodesReady(false)
    resetValidation()
    setDocument('')
    setFileName(file?.name ?? '')
    setError('')
    const id = files.current.next()
    if (!file) return
    if (file.size > PACKAGE_LIMIT) {
      setError('Plugin package exceeds the 1 MiB limit')
      return
    }
    try {
      const text = await readPluginFile(file)
      if (mounted.current && files.current.current(id)) setDocument(text)
    } catch {
      if (mounted.current && files.current.current(id))
        setError('Plugin package could not be read')
    }
  }
  async function validate() {
    const id = validation.current.next()
    setValidating(true)
    setValidated(undefined)
    setError('')
    try {
      const entry = await transport.validate({
        document,
        expected_sha256: expectedHash || undefined,
      })
      if (mounted.current && validation.current.current(id)) setValidated(entry)
    } catch (e) {
      if (mounted.current && validation.current.current(id))
        setError(
          e instanceof PluginError ? e.message : 'Task plugin request failed'
        )
    } finally {
      if (mounted.current && validation.current.current(id))
        setValidating(false)
    }
  }
  async function mutate(command: PluginMutation) {
    if (busy || unknown || !snapshot) return
    setConfirm(undefined)
    await catalog.mutate(command, () => {
      if (command.action === 'install') {
        setInstallOpen(false)
        resetValidation()
      }
      if (command.action === 'delete')
        setSelected((current) =>
          current === `${command.key}@${command.version}` ? undefined : current
        )
    })
  }
  const detail = snapshot?.entries.find(
    (e) => `${e.key}@${e.version}` === selected
  )
  const disabled = busy || loading || unknown || !snapshot
  const feedbackProps = {
    error,
    readError,
    notice,
    unknown,
    readAfterUnknown,
    busy,
    loading,
    onRefresh: () => {
      void refresh()
    },
    onContinue: catalog.continueFromObservation,
  }
  const status = (entry: PluginEntry): string => {
    if (entry.revoked) return t('Revoked')
    if (!entry.active) return t('Historical version')
    return entry.enabled ? t('Enabled') : t('Disabled')
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('API extension management')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            disabled={busy || loading}
            onClick={() => void refresh()}
          >
            <RefreshCw
              className={loading ? 'size-4 animate-spin' : 'size-4'}
              aria-hidden='true'
            />
            {t('Refresh')}
          </Button>
          <Button
            disabled={disabled}
            onClick={() => {
              setNodesReady(false)
              setInstallRevision(snapshot?.revision ?? '')
              setInstallOpen(true)
            }}
          >
            <Plus className='size-4' aria-hidden='true' />
            {t('Install plugin package')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='space-y-5'>
            <p className='text-muted-foreground bg-muted/30 flex items-start gap-2.5 rounded-lg border px-4 py-3 text-xs leading-relaxed'>
              <Info className='mt-0.5 size-4 shrink-0' aria-hidden='true' />
              {t(
                'Manage installed packages and versions. Enabling a package does not enable task execution or paid admission.'
              )}
            </p>
            {!selected && !installOpen && (
              <TaskPluginFeedback {...feedbackProps} />
            )}
            <PluginCatalog
              onSearchReady={onSearchReady}
              entries={snapshot?.entries ?? []}
              available={!!snapshot}
              loading={loading}
              status={status}
              onDetails={(entry) =>
                setSelected(`${entry.key}@${entry.version}`)
              }
            />
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PluginDetailSheet
        finalFocus={catalogSearch}
        selected={selected}
        setSelected={setSelected}
        detail={detail}
        status={status}
        disabled={disabled}
        busy={busy}
        loading={loading}
        installOpen={installOpen}
        feedbackProps={feedbackProps}
        confirm={confirm}
        setConfirm={setConfirm}
        nodesReady={nodesReady}
        setNodesReady={setNodesReady}
        snapshot={snapshot}
        mutate={mutate}
      />
      <PluginInstallSheet
        installOpen={installOpen}
        setInstallOpen={(open) => {
          if (!open) {
            files.current.next()
            setBundleLoading(false)
          }
          setInstallOpen(open)
        }}
        busy={busy}
        disabled={disabled}
        resetValidation={resetValidation}
        feedbackProps={feedbackProps}
        loadFile={loadFile}
        bundles={bundles}
        bundleLoading={bundleLoading}
        loadBundled={loadBundled}
        fileName={fileName}
        expectedHash={expectedHash}
        expectedHashLocked={bundledHash !== undefined}
        setExpectedHash={(value) => {
          if (bundledHash !== undefined) return
          if (bundleLoading) files.current.next()
          setBundleLoading(false)
          setExpectedHash(value)
        }}
        document={document}
        validating={validating}
        validate={validate}
        validated={validated}
        nodesReady={nodesReady}
        setNodesReady={setNodesReady}
        installRevision={installRevision}
        snapshot={snapshot}
        mutate={mutate}
      />
    </>
  )
}
