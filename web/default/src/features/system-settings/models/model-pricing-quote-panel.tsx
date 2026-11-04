// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import type {
  ModelPricingDraftSession,
  PricingDraftView,
} from './model-pricing-draft'

export function ModelPricingQuotePanel(props: {
  document: PricingDraftView
  session: ModelPricingDraftSession
  run: (action: () => Promise<boolean>) => void
}) {
  const { t, i18n } = useTranslation()
  const amountFormat = new Intl.NumberFormat(
    i18n.resolvedLanguage ?? i18n.language,
    { maximumSignificantDigits: 15 }
  )
  const d = props.document
  const context = d.base?.contexts.find((c) => c.id === d.source)
  const result = d.quote?.result
  const ready = Boolean(context?.available && context.contract)
  return (
    <section
      className='space-y-3 rounded-md border p-3'
      aria-label={t('Task quote sample')}
    >
      <h3 className='font-medium'>{t('Task quote sample')}</h3>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Server-calculated configuration sample in USD, excluding user-specific multipliers. It does not reserve funds or certify task admission.'
        )}
      </p>
      {!ready && (
        <p role='note' className='text-muted-foreground text-sm'>
          {t(
            'Select an available host source with a vetted phase contract to quote. A plugin schema alone is insufficient.'
          )}
        </p>
      )}
      <Textarea
        aria-label={t('Quote sample JSON')}
        rows={7}
        value={
          d.sampleText ??
          JSON.stringify(
            d.sample ?? { group: 'default', submit: {}, actual: {} },
            null,
            2
          )
        }
        onChange={(e) => props.session.editSample(e.target.value)}
      />
      {d.sampleText !== undefined && !d.sample && (
        <p role='alert'>{t('Invalid sample JSON or fields.')}</p>
      )}
      <Button
        variant='outline'
        disabled={
          !ready ||
          !d.sample ||
          d.writeState !== 'idle' ||
          Boolean(Object.keys(d.inputs ?? {}).length)
        }
        onClick={() => props.run(() => props.session.quote())}
      >
        {t('Calculate task quote')}
      </Button>
      {result && (
        <div
          className='space-y-2 text-sm break-words'
          aria-label={t('Quote result')}
        >
          <p>
            {t('Estimate')}: USD{' '}
            {amountFormat.format(result.preview.estimate.amount)}
          </p>
          <p>
            {t('Reserved quota')}: {result.preview.reserved_quota} (
            {t('Reservation floor')}: {result.reservation_floor})
          </p>
          <p>
            {t('Final amount')}:{' '}
            {result.preview.final
              ? `USD ${amountFormat.format(result.preview.final.amount)}`
              : t('Actual usage not provided')}
          </p>
          <p>
            {t('Final quota minus reserved')}:{' '}
            {result.preview.final_minus_reserved ??
              t('Actual usage not provided')}
          </p>
          <p>
            {t('Group ratio')}: {result.group_ratio}; {t('Quota per USD')}:{' '}
            {result.quota_per_unit}
          </p>
          <p>
            {t('Selected expression')}: {result.preview.selection.model} /{' '}
            {result.preview.selection.plugin_key ?? t('Default pricing')} —{' '}
            {result.preview.selection.expression}
          </p>
          <p>
            {t('Pricing time')}: {result.preview.estimate.pricing_at}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t('Price version')}: {result.price_version}; {t('Terms version')}:{' '}
            {result.terms_version}
          </p>
        </div>
      )}
    </section>
  )
}
