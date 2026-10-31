// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import type {
  ModelPricingDraftSession,
  PricingDraftView,
} from './model-pricing-draft'
import { modelPricingNumericKeys } from './model-pricing-types'

export function ModelPricingFields(props: {
  document: PricingDraftView
  session: ModelPricingDraftSession
}) {
  const { t } = useTranslation()
  const id = useId()
  const d = props.document
  if (!d.base || !d.pricing) return null
  const base = d.base
  const pricing = d.pricing
  const contexts = new Map(
    [...base.plugin_variants, ...base.contexts].map((c) => [c.id, c])
  )
  const selected = contexts.get(d.source)
  const plugin =
    selected?.plugin_key ??
    (d.source.startsWith('plugin:') ? d.source.slice(7) : '')
  const variants = pricing['billing_setting.plugin_billing_expr'] ?? {}
  const sources = new Map([...contexts.keys()].map((id) => [id, id]))
  for (const key of Object.keys(variants))
    sources.set(`plugin:${key}`, `plugin:${key}`)
  // Keep editing identity stable even after a saved deletion removes the stale
  // variant from the next entry. A source label must never silently become Default.
  if (d.source) sources.set(d.source, d.source)
  const setExpression = (text: string) => {
    if (!plugin)
      props.session.replacePricing({
        ...pricing,
        'billing_setting.billing_expr': text,
      })
    else {
      const next = { ...variants }
      if (text === '') delete next[plugin]
      else next[plugin] = text
      props.session.replacePricing({
        ...pricing,
        'billing_setting.plugin_billing_expr': next,
      })
    }
  }
  return (
    <section className='space-y-4' aria-label={t('Pricing configuration')}>
      <label className='grid gap-1 text-sm' htmlFor={`${id}-source`}>
        {t('Pricing source')}
        <select
          id={`${id}-source`}
          className='bg-background rounded-md border p-2'
          value={d.source}
          onChange={(e) => props.session.selectSource(e.target.value)}
        >
          <option value=''>{t('Default pricing')}</option>
          {[...sources].map(([value, label]) => (
            <option key={value} value={value}>
              {label}
            </option>
          ))}
        </select>
      </label>
      {selected && (
        <div className='text-muted-foreground space-y-1 text-xs'>
          <p>
            {t('Mapped model')}: {selected.mapped_model}
          </p>
          <p>
            {selected.reason ||
              t(
                'Configuration sample only; this does not activate task execution.'
              )}
          </p>
          <p>
            {t('Usage dimensions')}:{' '}
            {Object.entries(selected.usage_schema ?? {})
              .map(
                ([key, field]) =>
                  `${key} (${'unit' in field ? field.unit : field.type === 'boolean' ? 'boolean' : field.enum.join('/')}; ${selected.contract?.field_policy[key]?.phase ?? '?'})`
              )
              .join(', ') || t('None')}
          </p>
        </div>
      )}
      {!plugin && (
        <>
          <label className='grid gap-1 text-sm' htmlFor={`${id}-mode`}>
            {t('Billing mode')}
            <select
              id={`${id}-mode`}
              className='bg-background rounded-md border p-2'
              value={pricing['billing_setting.billing_mode'] ?? ''}
              onChange={(e) => {
                const next = { ...pricing }
                if (e.target.value === '')
                  delete next['billing_setting.billing_mode']
                else next['billing_setting.billing_mode'] = e.target.value
                props.session.replacePricing(next)
              }}
            >
              <option value=''>{t('Inherit')}</option>
              <option value='ratio'>{t('Ratio / fixed price')}</option>
              <option value='tiered_expr'>{t('Expression')}</option>
              {pricing['billing_setting.billing_mode'] &&
                !['ratio', 'tiered_expr'].includes(
                  pricing['billing_setting.billing_mode']
                ) && (
                  <option value={pricing['billing_setting.billing_mode']}>
                    {pricing['billing_setting.billing_mode']}
                  </option>
                )}
            </select>
          </label>
          <div className='grid gap-3 sm:grid-cols-2'>
            {modelPricingNumericKeys.map((key) => (
              <div key={key} className='grid gap-1 text-sm'>
                <label htmlFor={`${id}-${key}`}>
                  {key}
                  {key === 'ModelPrice'
                    ? ' (USD / request)'
                    : ` (${t('Ratio')})`}
                </label>
                <Input
                  id={`${id}-${key}`}
                  inputMode='decimal'
                  value={d.inputs?.[key] ?? pricing[key] ?? ''}
                  placeholder={t('Inherit')}
                  aria-invalid={d.inputs?.[key] !== undefined}
                  onChange={(e) =>
                    props.session.editNumber(key, e.target.value)
                  }
                />
                <span className='text-muted-foreground text-xs'>
                  {t('Effective value')}:{' '}
                  {String(base.effective[key] ?? t('Not configured'))}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
      <label className='grid gap-1 text-sm' htmlFor={`${id}-expression`}>
        {plugin ? t('Plugin expression override') : t('Default expression')}
        <Textarea
          id={`${id}-expression`}
          rows={4}
          value={
            plugin
              ? (variants[plugin] ?? '')
              : (pricing['billing_setting.billing_expr'] ?? '')
          }
          placeholder={
            plugin
              ? t(
                  'Empty inherits the default expression; 0 is explicitly free.'
                )
              : ''
          }
          onChange={(e) => setExpression(e.target.value)}
        />
      </label>
      {plugin && (
        <p className='text-muted-foreground text-xs'>
          {t('Effective expression')}:{' '}
          {selected?.effective ?? t('Not configured')}
        </p>
      )}
      <p className='text-muted-foreground text-xs'>
        {t(
          'Empty removes an override. Numeric 0 and expression 0 remain explicit zero. Task expressions return total USD; token expressions retain their existing units.'
        )}
      </p>
      <details>
        <summary className='cursor-pointer text-sm'>
          {t('Full model JSON / import')}
        </summary>
        <p className='text-muted-foreground my-2 text-xs'>
          {t(
            'This replaces the complete model draft, including hidden plugin overrides. It does not save automatically.'
          )}
        </p>
        <Textarea
          aria-label={t('Full model JSON / import')}
          rows={10}
          value={d.inputs?.$json ?? JSON.stringify(pricing, null, 2)}
          onChange={(e) => props.session.importPricing(e.target.value)}
        />
      </details>
    </section>
  )
}
