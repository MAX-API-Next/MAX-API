// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Selective workflow reference: QuantumNous/new-api 70e6af64 plugin list,
// upload and detail sheet. No upstream JS execution/editor/marketplace code.
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import {
  taskPluginAPI,
  PluginError,
  PluginRequestOrder,
  type TaskPluginAPI,
  type PluginEntry,
  type PluginMutation,
} from './api'
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
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<string>()
  const [installOpen, setInstallOpen] = useState(false)
  const [installRevision, setInstallRevision] = useState('')
  const [document, setDocument] = useState('')
  const [fileName, setFileName] = useState('')
  const [expectedHash, setExpectedHash] = useState('')
  const [validated, setValidated] = useState<PluginEntry>()
  const [validating, setValidating] = useState(false)
  const [nodesReady, setNodesReady] = useState(false)
  const [confirm, setConfirm] = useState<PluginConfirmation>()
  const validation = useRef(new PluginRequestOrder())
  const files = useRef(new PluginRequestOrder())
  const mounted = useRef(true)
  const resetValidation = () => {
    validation.current.next()
    setValidated(undefined)
    setValidating(false)
  }
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

  async function loadFile(file: File | undefined) {
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
      const text = await file.text()
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
        expected_sha256: expectedHash,
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
    })
  }
  const detail = snapshot?.entries.find(
    (e) => `${e.key}@${e.version}` === selected
  )
  const rows =
    snapshot?.entries.filter((e) =>
      `${e.key} ${e.name} ${e.version} ${e.models.join(' ')}`
        .toLowerCase()
        .includes(search.toLowerCase())
    ) ?? []
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
        <SectionPageLayout.Title>{t('Task Plugins')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            disabled={busy || loading}
            onClick={() => void refresh()}
          >
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
            {t('Install plugin package')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='space-y-4'>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Manage installed packages and versions. Enabling a package does not enable task execution or paid admission.'
              )}
            </p>
            {!selected && !installOpen && (
              <TaskPluginFeedback {...feedbackProps} />
            )}
            <Input
              aria-label={t('Search task plugins')}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={t('Search task plugins')}
            />
            {loading && <p role='status'>{t('Loading...')}</p>}
            {!loading && snapshot && rows.length === 0 && (
              <p>{t('No task plugin packages found')}</p>
            )}
            <div className='overflow-x-auto rounded-md border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Plugin')}</TableHead>
                    <TableHead>{t('Version')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Models')}</TableHead>
                    <TableHead>{t('Protocols')}</TableHead>
                    <TableHead>{t('Execution')}</TableHead>
                    <TableHead>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((entry) => (
                    <TableRow key={`${entry.key}@${entry.version}`}>
                      <TableCell>
                        <div className='font-medium'>{entry.name}</div>
                        <div className='text-muted-foreground text-xs'>
                          {entry.key}
                        </div>
                      </TableCell>
                      <TableCell>{entry.version}</TableCell>
                      <TableCell>
                        <Badge variant='outline'>{status(entry)}</Badge>
                      </TableCell>
                      <TableCell>{entry.models.length}</TableCell>
                      <TableCell>
                        {entry.protocols.map((p) => p.name).join(', ') || '—'}
                      </TableCell>
                      <TableCell>{t('Execution not ready')}</TableCell>
                      <TableCell>
                        <Button
                          size='sm'
                          variant='outline'
                          onClick={() =>
                            setSelected(`${entry.key}@${entry.version}`)
                          }
                        >
                          {t('Details')}
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PluginDetailSheet
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
        setInstallOpen={setInstallOpen}
        busy={busy}
        disabled={disabled}
        resetValidation={resetValidation}
        feedbackProps={feedbackProps}
        loadFile={loadFile}
        fileName={fileName}
        expectedHash={expectedHash}
        setExpectedHash={setExpectedHash}
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
