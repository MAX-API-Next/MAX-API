// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Alert, AlertDescription } from '@/components/ui/alert'
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
import { ConfirmDialog } from '@/components/confirm-dialog'
import { getModelPricing } from './model-pricing-api'
import { ModelPricingPanel } from './model-pricing-panel'
import { invalidateModelPricing } from './model-pricing-queries'
import type {
  ModelPricingEntry,
  ModelPricingValues,
} from './model-pricing-types'
import { useModelPricingDraft } from './use-model-pricing-draft'

type Batch = { entries: ModelPricingEntry[]; pricing?: ModelPricingValues }
export function ModelPricingWorkspace(props: {
  variant?: 'default' | 'unset'
  active?: boolean
}) {
  const { t } = useTranslation()
  const revision = useAuthStore((s) => s.sessionRevision)
  const root = useAuthStore((s) => s.auth.user?.role === ROLE.SUPER_ADMIN)
  if (!root)
    return <p>{t('Model pricing is managed by a super administrator.')}</p>
  return (
    <PricingWorkspace
      key={revision}
      revision={revision}
      variant={props.variant}
      active={props.active ?? true}
    />
  )
}
function PricingWorkspace(props: {
  revision: number
  variant?: 'default' | 'unset'
  active: boolean
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [model, setModel] = useState('')
  const [search, setSearch] = useState('')
  const [name, setName] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [limit, setLimit] = useState(100)
  const [batch, setBatch] = useState<Batch | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const draft = useModelPricingDraft(model, undefined, props.active)
  const query = useQuery({
    queryKey: ['model-pricing-config', props.revision],
    queryFn: ({ signal }) => getModelPricing([], signal),
    retry: false,
    refetchOnWindowFocus: false,
    enabled: props.active,
  })
  const entries = query.data?.entries ?? []
  const errorMessage = error || query.error?.message || ''
  const visible = entries.filter(
    (e) =>
      e.model_name.toLowerCase().includes(search.toLowerCase()) &&
      (props.variant !== 'unset' ||
        (e.configured.ModelPrice === undefined &&
          e.configured.ModelRatio === undefined &&
          e.configured['billing_setting.billing_mode'] !== 'tiered_expr'))
  )
  const blocked =
    busy ||
    !draft.document?.pricing ||
    draft.document.writeState !== 'idle' ||
    Boolean(Object.keys(draft.document.inputs ?? {}).length) ||
    draft.blockedModels.length > 0
  const prepare = (reset: boolean) => {
    const targets = entries.filter(
      (e) =>
        selected.includes(e.model_name) && (reset || e.model_name !== model)
    )
    if (!targets.length) {
      setError(t('Select target models first.'))
      return
    }
    setBatch({
      entries: structuredClone(targets),
      pricing: reset ? undefined : structuredClone(draft.document!.pricing!),
    })
  }
  const executeBatch = async () => {
    if (!batch) return
    const pending = batch
    setBatch(null)
    setBusy(true)
    setError('')
    try {
      await draft.session.saveBatch(pending.entries, pending.pricing)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('Request failed'))
    } finally {
      setBusy(false)
      if (draft.session.getSnapshot().model !== null)
        invalidateModelPricing(client)
    }
  }
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'Edit one authoritative model configuration at a time. Refresh keeps unsaved drafts; copying includes all plugin overrides. Prices do not activate task execution.'
        )}
      </p>
      <div className='flex flex-wrap gap-2'>
        <Input
          className='min-w-48 flex-1'
          aria-label={t('Search model pricing')}
          placeholder={t('Search model pricing')}
          value={search}
          onChange={(e) => {
            setSearch(e.target.value)
            setLimit(100)
          }}
        />
        <Button
          variant='outline'
          disabled={query.isFetching}
          onClick={() => void query.refetch()}
        >
          {t('Refresh model list')}
        </Button>
        <Input
          className='min-w-48 flex-1'
          aria-label={t('Model name')}
          placeholder={t('Model name')}
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <Button
          variant='outline'
          disabled={!name.trim() || name.trim().length > 256}
          onClick={() => setModel(name.trim())}
        >
          {t('Open model')}
        </Button>
      </div>
      {query.isPending && <p role='status'>{t('Loading...')}</p>}
      {(query.isError || error) && (
        <Alert variant='destructive'>
          <AlertDescription role='alert'>
            {t(errorMessage, {
              defaultValue: errorMessage,
              keySeparator: false,
              nsSeparator: false,
            })}
          </AlertDescription>
        </Alert>
      )}
      {draft.cacheWarningModels.length > 0 && (
        <Alert>
          <AlertDescription>
            {t('Committed models with local cache publication warnings')}:{' '}
            {draft.cacheWarningModels.join(', ')}
          </AlertDescription>
        </Alert>
      )}
      {draft.blockedModels.length > 0 && (
        <Alert>
          <AlertDescription className='space-y-2'>
            <p>
              {t('Resolve these model writes before another batch')}:{' '}
              {draft.blockedModels.join(', ')}
            </p>
            <Button
              variant='outline'
              disabled={busy}
              onClick={() => {
                void draft.session
                  .reconcileModels(draft.blockedModels)
                  .catch((e) =>
                    setError(
                      e instanceof Error ? e.message : t('Request failed')
                    )
                  )
              }}
            >
              {t('Recheck unresolved writes')}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      <div className='grid items-start gap-4 lg:grid-cols-[minmax(16rem,1fr)_minmax(0,2fr)]'>
        <div className='space-y-3'>
          <div className='flex flex-wrap gap-2'>
            <Button
              variant='outline'
              disabled={blocked || !selected.some((s) => s !== model)}
              onClick={() => prepare(false)}
            >
              {t('Copy pricing to selected')}
            </Button>
            <Button
              variant='outline'
              disabled={blocked || !selected.length}
              onClick={() => prepare(true)}
            >
              {t('Reset selected pricing')}
            </Button>
            <Button
              variant='ghost'
              disabled={!selected.length}
              onClick={() => setSelected([])}
            >
              {t('Clear selection')} ({selected.length})
            </Button>
          </div>
          <div className='max-h-[70vh] overflow-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Select')}</TableHead>
                  <TableHead>{t('Model name')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.slice(0, limit).map((entry) => (
                  <TableRow
                    key={entry.model_name}
                    data-state={
                      model === entry.model_name ? 'selected' : undefined
                    }
                  >
                    <TableCell>
                      <input
                        type='checkbox'
                        aria-label={`${t('Select')} ${entry.model_name}`}
                        checked={selected.includes(entry.model_name)}
                        onChange={(e) =>
                          setSelected((previous) =>
                            e.target.checked
                              ? [...previous, entry.model_name]
                              : previous.filter((n) => n !== entry.model_name)
                          )
                        }
                      />
                    </TableCell>
                    <TableCell>
                      <Button
                        variant='ghost'
                        className='h-auto text-start break-all whitespace-normal'
                        onClick={() => setModel(entry.model_name)}
                      >
                        {entry.model_name}
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <p className='text-muted-foreground text-xs'>
            {Math.min(limit, visible.length)} / {visible.length}
          </p>
          {visible.length > limit && (
            <Button variant='outline' onClick={() => setLimit((n) => n + 100)}>
              {t('Show more')}
            </Button>
          )}
        </div>
        {model ? (
          <ModelPricingPanel
            key={model}
            model={model}
            document={draft.document}
            session={draft.session}
          />
        ) : (
          <p className='text-muted-foreground p-4'>
            {t('Select a model to edit pricing')}
          </p>
        )}
      </div>
      <ConfirmDialog
        open={batch !== null}
        onOpenChange={(open) => {
          if (!open) setBatch(null)
        }}
        title={
          batch?.pricing === undefined
            ? t('Reset selected pricing')
            : t('Copy pricing to selected')
        }
        desc={
          <div className='space-y-2'>
            <p>
              {t(
                'Replace the complete configuration of these models atomically, including plugin overrides. Existing dirty target drafts must be resolved first.'
              )}
            </p>
            <p className='break-all'>
              {batch?.entries.map((e) => e.model_name).join(', ')}
            </p>
          </div>
        }
        handleConfirm={() => void executeBatch()}
      />
    </div>
  )
}
