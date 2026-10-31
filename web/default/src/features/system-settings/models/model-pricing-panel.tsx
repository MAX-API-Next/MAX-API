// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// UI organization references New API 1424ffdd model-pricing-panel.tsx (AGPL-3.0).
// MAX owns the draft state, three guards and server-only sample calculations.
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import type {
  ModelPricingDraftSession,
  PricingDraftView,
} from './model-pricing-draft'
import { ModelPricingFields } from './model-pricing-fields'
import { invalidateModelPricing } from './model-pricing-queries'
import { ModelPricingQuotePanel } from './model-pricing-quote-panel'

export function ModelPricingPanel(props: {
  model: string
  document?: PricingDraftView
  session: ModelPricingDraftSession
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [confirm, setConfirm] = useState<'reset' | 'discard' | null>(null)
  const [error, setError] = useState('')
  const d = props.document
  const run = async (action: () => Promise<boolean>, write = false) => {
    setError('')
    try {
      await action()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('Request failed'))
    } finally {
      if (write && props.session.getSnapshot().model !== null)
        invalidateModelPricing(client)
    }
  }
  const invalid = Boolean(Object.keys(d?.inputs ?? {}).length)
  const blocked = !d?.base || d.loading || d.writeState !== 'idle' || invalid
  return (
    <section
      className='min-w-0 space-y-4 rounded-lg border p-4'
      aria-label={t('Model pricing editor')}
    >
      <h2 className='font-medium break-all'>{props.model}</h2>
      <div className='flex flex-wrap gap-2'>
        <Button
          variant='outline'
          disabled={d?.writeState === 'saving'}
          onClick={() => void run(() => props.session.refresh())}
        >
          {t('Refresh pricing')}
        </Button>
        <Button
          disabled={blocked || !d?.dirty}
          onClick={() => void run(() => props.session.save(), true)}
        >
          {t('Save model pricing')}
        </Button>
        <Button
          variant='outline'
          disabled={blocked}
          onClick={() => void run(() => props.session.preview())}
        >
          {t('Preview configuration')}
        </Button>
        <Button
          variant='outline'
          disabled={blocked}
          onClick={() => setConfirm('reset')}
        >
          {t('Restore default pricing')}
        </Button>
        <Button
          variant='outline'
          disabled={
            !d?.base ||
            d.writeState === 'saving' ||
            (!d.remote && d.writeState !== 'idle') ||
            (!d.dirty && !d.remote)
          }
          onClick={() => setConfirm('discard')}
        >
          {t('Discard draft')}
        </Button>
      </div>
      {d?.loading && <p role='status'>{t('Loading...')}</p>}
      {d && (
        <p role='status' className='text-muted-foreground text-sm'>
          {d.writeState === 'saving' && t('Saving...')}
          {d.writeState === 'unknown' &&
            t(
              'Save result unresolved. Refresh authoritative pricing; do not resend automatically.'
            )}
          {d.writeState === 'conflict' &&
            t(
              'Pricing changed. Your draft is retained; refresh and explicitly adopt the observed configuration.'
            )}
          {d.writeState === 'idle' &&
            (d.dirty
              ? t('Unsaved changes')
              : t('Authoritative configuration observed.'))}
        </p>
      )}
      {(error || d?.error || d?.base?.configuration_error || invalid) && (
        <Alert variant='destructive'>
          <AlertDescription role='alert'>
            {error ||
              d?.error?.message ||
              d?.base?.configuration_error ||
              t(
                'Complete or correct invalid pricing inputs before saving or previewing.'
              )}
          </AlertDescription>
        </Alert>
      )}
      {d?.commitKnown && (
        <p className='text-sm'>{t('Database commit acknowledged.')}</p>
      )}
      {d?.cachePublished === false && (
        <Alert>
          <AlertDescription>
            {t(
              'Database committed; local cache publication failed. This is not a rollback or proof of multi-node cutover.'
            )}{' '}
            {d.warning}
          </AlertDescription>
        </Alert>
      )}
      {d?.base && (
        <p className='text-muted-foreground text-xs break-all'>
          {t('Price version')}: {d.base.version}
        </p>
      )}
      {d?.pricing && (
        <>
          <ModelPricingFields document={d} session={props.session} />
          {d.preview && (
            <details open>
              <summary>{t('Configuration preview')}</summary>
              <pre className='max-h-64 overflow-auto text-xs whitespace-pre-wrap'>
                {JSON.stringify(
                  {
                    configured: d.preview.configured,
                    effective: d.preview.effective,
                    contexts: d.preview.contexts,
                    draft_version: d.preview.draft_version,
                  },
                  null,
                  2
                )}
              </pre>
            </details>
          )}
          <ModelPricingQuotePanel
            document={d}
            session={props.session}
            run={(action) => void run(action)}
          />
        </>
      )}
      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(open) => {
          if (!open) setConfirm(null)
        }}
        title={
          confirm === 'reset'
            ? t('Restore default pricing')
            : t('Discard draft')
        }
        desc={
          confirm === 'reset'
            ? t(
                'Replace this model configuration with compiled defaults. Plugin overrides are removed. Other models are unchanged.'
              )
            : t(
                'Discard local inputs and adopt the last observed configuration. An unresolved earlier request may still complete; this does not prove it failed.'
              )
        }
        handleConfirm={() => {
          const action = confirm
          setConfirm(null)
          if (action === 'reset') void run(() => props.session.save(true), true)
          else {
            try {
              if (d?.remote) props.session.adoptRemote()
              else props.session.discardLocal()
            } catch (e) {
              setError(String(e))
            }
          }
        }}
      />
    </section>
  )
}
