// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Selective workflow reference: QuantumNous/new-api 70e6af64 plugin list,
// upload and detail sheet. No upstream JS execution/editor/marketplace code.
import { useCallback, useEffect, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import { invalidateModelPricing } from '@/features/system-settings/models/model-pricing-queries'
import {
  taskPluginAPI,
  PluginError,
  PluginRequestOrder,
  type TaskPluginAPI,
  type PluginEntry,
  type PluginSnapshot,
  type PluginMutation,
  type PluginAction,
} from './api'
import { TaskPluginFeedback } from './task-plugin-feedback'

const PACKAGE_LIMIT = 1024 * 1024
export function TaskPlugins({
  transport = taskPluginAPI,
}: {
  transport?: TaskPluginAPI
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [snapshot, setSnapshot] = useState<PluginSnapshot>()
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [readError, setReadError] = useState('')
  const [notice, setNotice] = useState('')
  const [unknown, setUnknown] = useState(false)
  const [readAfterUnknown, setReadAfterUnknown] = useState(false)
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
  const [confirm, setConfirm] = useState<{
    action: PluginAction
    entry: PluginEntry
    revision: string
  }>()
  const reads = useRef(new PluginRequestOrder())
  const validation = useRef(new PluginRequestOrder())
  const files = useRef(new PluginRequestOrder())
  const unknownRef = useRef(false)
  const mounted = useRef(true)
  const writing = useRef(false)
  const resetValidation = () => {
    validation.current.next()
    setValidated(undefined)
    setValidating(false)
  }
  const readSnapshot = useCallback(
    async (id: number) => {
      try {
        const fresh = await transport.read()
        if (mounted.current && reads.current.current(id)) {
          setSnapshot(fresh)
          setReadError('')
          setReadAfterUnknown(unknownRef.current)
        }
      } catch {
        if (mounted.current && reads.current.current(id)) {
          setSnapshot(undefined)
          setReadError('Task plugin catalog could not be loaded')
          setReadAfterUnknown(false)
        }
      } finally {
        if (mounted.current && reads.current.current(id)) setLoading(false)
      }
    },
    [transport]
  )
  const refresh = useCallback(() => {
    setLoading(true)
    return readSnapshot(reads.current.next())
  }, [readSnapshot])
  useEffect(() => {
    mounted.current = true
    const readOrder = reads.current
    const validationOrder = validation.current
    const fileOrder = files.current
    void readSnapshot(readOrder.next())
    return () => {
      mounted.current = false
      readOrder.next()
      validationOrder.next()
      fileOrder.next()
    }
  }, [readSnapshot])

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
    if (writing.current || unknownRef.current || !snapshot) return
    writing.current = true
    reads.current.next() // Invalidate any GET begun before this write.
    setBusy(true)
    setError('')
    setNotice('')
    setConfirm(undefined)
    try {
      await transport.mutate(command)
      if (!mounted.current) return
      setNotice(
        'Plugin catalog updated. Execution and paid admission remain unavailable.'
      )
      if (command.action === 'install') {
        setInstallOpen(false)
        resetValidation()
      }
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
    onContinue: () => {
      if (!readAfterUnknown || busy || loading) return
      unknownRef.current = false
      setUnknown(false)
      setNotice(
        'Continue using the observed catalog revision; the previous result is not certified.'
      )
    },
  }
  const status = (e: PluginEntry) =>
    e.revoked
      ? t('Revoked')
      : !e.active
        ? t('Historical version')
        : e.enabled
          ? t('Enabled')
          : t('Disabled')

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

      <Sheet
        open={!!selected}
        onOpenChange={(open) => {
          if (!open) setSelected(undefined)
        }}
      >
        <SheetContent className='w-full overflow-y-auto sm:max-w-2xl'>
          <SheetHeader>
            <SheetTitle>{t('Plugin details')}</SheetTitle>
            <SheetDescription>
              {t(
                'Declared metadata and immutable version history; execution is not available.'
              )}
            </SheetDescription>
          </SheetHeader>
          <div className='space-y-5 px-4 pb-6'>
            {!installOpen && (
              <TaskPluginFeedback {...feedbackProps} showRefresh />
            )}
            {detail ? (
              <>
                <h3 className='font-semibold'>
                  {detail.name} · {detail.version}
                </h3>
                <p>
                  {status(detail)} · {t('Package format')}{' '}
                  {detail.format_version}
                </p>
                <p className='font-mono text-xs break-all'>
                  SHA-256: {detail.digest}
                </p>
                <p>
                  {t(
                    'Disabling blocks new bindings. Revoking permanently rejects this version and its historical sessions.'
                  )}
                </p>
                <div className='flex flex-wrap gap-2'>
                  {!detail.revoked && (
                    <>
                      {!detail.active && (
                        <Button
                          disabled={disabled}
                          onClick={() => {
                            setNodesReady(false)
                            setConfirm({
                              action: 'activate',
                              entry: detail,
                              revision: snapshot?.revision ?? '',
                            })
                          }}
                        >
                          {t('Activate version')}
                        </Button>
                      )}
                      {detail.active && (
                        <Button
                          disabled={disabled}
                          variant='outline'
                          onClick={() =>
                            setConfirm({
                              action: detail.enabled ? 'disable' : 'enable',
                              entry: detail,
                              revision: snapshot?.revision ?? '',
                            })
                          }
                        >
                          {detail.enabled ? t('Disable') : t('Enable')}
                        </Button>
                      )}
                      <Button
                        disabled={disabled}
                        variant='destructive'
                        onClick={() =>
                          setConfirm({
                            action: 'revoke',
                            entry: detail,
                            revision: snapshot?.revision ?? '',
                          })
                        }
                      >
                        {t('Revoke version')}
                      </Button>
                    </>
                  )}
                </div>
                {confirm && confirm.entry.digest === detail.digest && (
                  <div className='space-y-3 rounded-md border p-3'>
                    <p>
                      {t('Confirm catalog operation')}: {t(confirm.action)} ·{' '}
                      {detail.key}@{detail.version}
                    </p>
                    {confirm.action === 'activate' &&
                      detail.format_version === 2 && (
                        <label className='flex items-start gap-2'>
                          <input
                            type='checkbox'
                            checked={nodesReady}
                            onChange={(e) => setNodesReady(e.target.checked)}
                          />
                          {t(
                            'All catalog nodes have been upgraded for format 2. Older nodes may suspend their entire plugin host.'
                          )}
                        </label>
                      )}
                    {confirm.revision !== snapshot?.revision && (
                      <p role='alert'>
                        {t(
                          'Catalog changed; reopen the confirmation before continuing.'
                        )}
                      </p>
                    )}
                    <Button
                      disabled={
                        disabled ||
                        confirm.revision !== snapshot?.revision ||
                        (confirm.action === 'activate' &&
                          detail.format_version === 2 &&
                          !nodesReady)
                      }
                      onClick={() =>
                        void mutate({
                          action: confirm.action,
                          key: confirm.entry.key,
                          version: confirm.entry.version,
                          expected_revision: confirm.revision,
                          format2_nodes_ready: nodesReady,
                        })
                      }
                    >
                      {t('Confirm')}
                    </Button>
                    <Button
                      variant='outline'
                      onClick={() => setConfirm(undefined)}
                    >
                      {t('Cancel')}
                    </Button>
                  </div>
                )}
                <h4 className='font-medium'>{t('Declared models')}</h4>
                <ul className='space-y-1'>
                  {detail.models.map((model) => (
                    <li key={model} className='break-all'>
                      {model}
                    </li>
                  ))}
                </ul>
                <a
                  className='underline'
                  href='/system-settings/billing/model-pricing'
                >
                  {t('Open model and source pricing')}
                </a>
                <h4 className='font-medium'>
                  {t('Declared protocols and routes')}
                </h4>
                <ul>
                  {detail.protocols.map((p) => (
                    <li key={p.name}>
                      {p.name} {p.supports.join(', ')}
                    </li>
                  ))}
                  {detail.routes.map((r, i) => (
                    <li key={i} className='break-all'>
                      {r.method} {r.path} · {r.type}
                    </li>
                  ))}
                </ul>
                <h4 className='font-medium'>{t('Model usage schemas')}</h4>
                {(detail.usage_profiles.length
                  ? detail.usage_profiles
                  : [{ models: detail.models, schema: detail.usage_schema }]
                ).map((profile, i) => (
                  <div key={i} className='space-y-2 rounded-md border p-3'>
                    <p className='text-muted-foreground text-xs break-all'>
                      {profile.models.join(', ')}
                    </p>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>{t('Field')}</TableHead>
                          <TableHead>{t('Type')}</TableHead>
                          <TableHead>{t('Unit / values')}</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {Object.entries(profile.schema).map(([key, field]) => (
                          <TableRow key={key}>
                            <TableCell>{key}</TableCell>
                            <TableCell>{field.type}</TableCell>
                            <TableCell>
                              {field.unit || field.enum?.join(', ') || '—'}
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                ))}
              </>
            ) : (
              snapshot && (
                <p>{t('The selected version is not in the current catalog')}</p>
              )
            )}
          </div>
        </SheetContent>
      </Sheet>

      <Sheet
        open={installOpen}
        onOpenChange={(open) => {
          if (!busy) {
            setInstallOpen(open)
            if (!open) resetValidation()
          }
        }}
      >
        <SheetContent className='w-full overflow-y-auto sm:max-w-xl'>
          <SheetHeader>
            <SheetTitle>{t('Install plugin package')}</SheetTitle>
            <SheetDescription>
              {t(
                'Select a MAX package document. Installation retains immutable bytes and activates this version; it does not execute JavaScript.'
              )}
            </SheetDescription>
          </SheetHeader>
          <div className='space-y-4 px-4 pb-6'>
            <TaskPluginFeedback {...feedbackProps} showRefresh />
            <Label htmlFor='plugin-file'>{t('Package file')}</Label>
            <Input
              id='plugin-file'
              type='file'
              accept='.json,application/json'
              disabled={busy}
              onChange={(e) => void loadFile(e.target.files?.[0])}
            />
            <p className='text-muted-foreground text-xs break-all'>
              {fileName}
            </p>
            <Label htmlFor='plugin-pin'>{t('Trusted package SHA-256')}</Label>
            <Input
              id='plugin-pin'
              value={expectedHash}
              disabled={busy}
              onChange={(e) => {
                setExpectedHash(e.target.value.trim().toLowerCase())
                resetValidation()
              }}
            />
            <p className='text-muted-foreground text-sm'>
              {t(
                'Use a digest from a trusted publisher. A digest calculated from the uploaded file alone does not prove authenticity. Maximum package size: 1 MiB.'
              )}
            </p>
            <Button
              variant='outline'
              disabled={
                busy ||
                validating ||
                !document ||
                !/^[a-f0-9]{64}$/.test(expectedHash)
              }
              onClick={() => void validate()}
            >
              {t('Validate package')}
            </Button>
            {validated && (
              <div className='space-y-3 rounded-md border p-3'>
                <p>
                  {validated.name} · {validated.key}@{validated.version}
                </p>
                <p>
                  {t('Package format')} {validated.format_version} ·{' '}
                  {validated.models.length} {t('Models')}
                </p>
                {validated.format_version === 2 && (
                  <label className='flex items-start gap-2'>
                    <input
                      type='checkbox'
                      checked={nodesReady}
                      onChange={(e) => setNodesReady(e.target.checked)}
                    />
                    {t(
                      'All catalog nodes have been upgraded for format 2. Older nodes may suspend their entire plugin host.'
                    )}
                  </label>
                )}
                {installRevision !== snapshot?.revision && (
                  <p role='alert'>
                    {t(
                      'Catalog changed; reopen the confirmation before continuing.'
                    )}
                  </p>
                )}
                <Button
                  disabled={
                    disabled ||
                    installRevision !== snapshot?.revision ||
                    (validated.format_version === 2 && !nodesReady)
                  }
                  onClick={() =>
                    void mutate({
                      action: 'install',
                      document,
                      expected_sha256: expectedHash,
                      expected_revision: installRevision,
                      format2_nodes_ready: nodesReady,
                    })
                  }
                >
                  {t('Install and activate')}
                </Button>
              </div>
            )}
          </div>
        </SheetContent>
      </Sheet>
    </>
  )
}
