/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
import { memo, useCallback, useEffect, useMemo, useState } from 'react'
import { FileJson } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { CopyButton } from '@/components/copy-button'
import { SettingsPageActionsPortal } from '../components/settings-page-context'
import { useUpdateOption } from '../hooks/use-update-option'
import { formatJsonForTextarea, normalizeJsonString } from './utils'

const OPTION_KEY = 'task_billing_setting.rate_cards'

const VENDOR_LABELS: Record<string, string> = {
  kling: 'Kling',
  minimax: 'MiniMax',
  openai: 'OpenAI / Sora',
  google: 'Google / Veo',
  bytedance: 'ByteDance / Seedance',
  unclassified: 'Unclassified',
}

const VENDOR_ORDER = [
  'kling',
  'minimax',
  'openai',
  'google',
  'bytedance',
  'unclassified',
]

const KLING_RATE_CARD_EXAMPLE = JSON.stringify(
  {
    'kling/kling-v3-video-generation': {
      vendor: 'kling',
      unit: 'second',
      quantity_field: 'duration',
      default_quantity: 5,
      strict: true,
      defaults: {
        quality: 'std',
        has_audio: 'false',
      },
      rows: [
        {
          id: 'std_no_audio',
          match: {
            quality: 'std',
            has_audio: 'false',
          },
          unit_price: 0.6,
        },
        {
          id: 'pro_audio',
          match: {
            quality: 'pro',
            has_audio: 'true',
          },
          unit_price: 1.2,
        },
      ],
    },
    'kling/kling-v3-omni-video-generation': {
      vendor: 'kling',
      unit: 'second',
      quantity_field: 'duration',
      default_quantity: 5,
      strict: true,
      defaults: {
        quality: 'std',
        has_video_input: 'false',
        has_audio: 'false',
      },
      rows: [
        {
          id: 'std_no_video_no_audio',
          match: {
            quality: 'std',
            has_video_input: 'false',
            has_audio: 'false',
          },
          unit_price: 0.6,
        },
        {
          id: 'pro_video_no_audio',
          match: {
            quality: 'pro',
            has_video_input: 'true',
            has_audio: 'false',
          },
          unit_price: 1.2,
        },
      ],
    },
  },
  null,
  2
)

const MINIMAX_RATE_CARD_EXAMPLE = JSON.stringify(
  {
    'minimax/minimax-h3': {
      vendor: 'minimax',
      billing_type: 'minimax',
      billing_config: {
        schema_version: 1,
        mode: 'bounded_actual',
        currency: 'USD',
        output_unit_price: {
          '768P': '0.08',
          '2K': '0.13',
        },
        input_video_unit_price: {
          '768P': '0.08',
          '2K': '0.13',
        },
        input_video_max_seconds: 15,
        input_image_free_count: 5,
        input_image_extra_unit_price: '0.04',
        input_audio_unit_price: '0',
      },
    },
  },
  null,
  2
)

type TaskRateCardSettingsProps = {
  defaultValue: string
}

type VendorSummary = {
  key: string
  label: string
  modelCount: number
  rowCount: number
  models: string[]
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function normalizeVendor(value: string) {
  return value.trim().toLowerCase()
}

function inferVendor(model: string, card: Record<string, unknown>) {
  const configuredVendor =
    typeof card.vendor === 'string' ? normalizeVendor(card.vendor) : ''
  if (configuredVendor) {
    return configuredVendor
  }

  const normalizedModel = normalizeVendor(model)
  if (normalizedModel.includes('kling')) return 'kling'
  if (normalizedModel.includes('minimax')) return 'minimax'
  if (normalizedModel.includes('sora') || normalizedModel.includes('openai')) {
    return 'openai'
  }
  if (normalizedModel.includes('veo') || normalizedModel.includes('google')) {
    return 'google'
  }
  if (
    normalizedModel.includes('seedance') ||
    normalizedModel.includes('bytedance') ||
    normalizedModel.includes('doubao')
  ) {
    return 'bytedance'
  }
  return 'unclassified'
}

function sortVendors(a: VendorSummary, b: VendorSummary) {
  const aIndex = VENDOR_ORDER.indexOf(a.key)
  const bIndex = VENDOR_ORDER.indexOf(b.key)
  const normalizedAIndex = aIndex === -1 ? VENDOR_ORDER.length : aIndex
  const normalizedBIndex = bIndex === -1 ? VENDOR_ORDER.length : bIndex
  if (normalizedAIndex !== normalizedBIndex) {
    return normalizedAIndex - normalizedBIndex
  }
  return a.label.localeCompare(b.label)
}

function buildVendorSummary(value: string): VendorSummary[] {
  const trimmed = value.trim()
  let parsed: unknown

  try {
    parsed = trimmed ? JSON.parse(trimmed) : {}
  } catch {
    return []
  }

  if (!isRecord(parsed)) {
    return []
  }

  const groups = new Map<string, VendorSummary>()

  for (const [model, rawCard] of Object.entries(parsed)) {
    const card = isRecord(rawCard) ? rawCard : {}
    const vendor = inferVendor(model, card)
    const rows = Array.isArray(card.rows) ? card.rows.length : 0
    const group = groups.get(vendor) ?? {
      key: vendor,
      label: VENDOR_LABELS[vendor] ?? vendor,
      modelCount: 0,
      rowCount: 0,
      models: [],
    }

    group.modelCount += 1
    group.rowCount += rows
    group.models.push(model)
    groups.set(vendor, group)
  }

  return Array.from(groups.values()).sort(sortVendors)
}

export const TaskRateCardSettings = memo(function TaskRateCardSettings({
  defaultValue,
}: TaskRateCardSettingsProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const vendorSummary = useMemo(() => buildVendorSummary(text), [text])

  useEffect(() => {
    setText(formatJsonForTextarea(defaultValue || '{}'))
    setError('')
  }, [defaultValue])

  const handleChange = useCallback(
    (value: string) => {
      setText(value)
      try {
        const parsed = JSON.parse(value) as unknown
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          setError(t('JSON must be an object'))
          return
        }
        setError('')
      } catch (err) {
        setError(err instanceof Error ? err.message : t('Invalid JSON'))
      }
    },
    [t]
  )

  const handleUseExample = useCallback(
    (example: string) => {
      setText(example)
      setError('')
      toast.success(t('Example loaded. Review prices before saving.'))
    },
    [t]
  )

  const handleSave = useCallback(async () => {
    if (error) {
      toast.error(error)
      return
    }
    await updateOption.mutateAsync({
      key: OPTION_KEY,
      value: normalizeJsonString(text || '{}'),
    })
  }, [error, text, updateOption])

  return (
    <div className='space-y-4'>
      <Alert>
        <AlertDescription className='space-y-2'>
          <p>
            {t(
              'Configure task billing rate cards as JSON. Matching rows override the base per-request price; non-matching strict cards are rejected.'
            )}
          </p>
          <p className='text-xs'>
            {t('Configuration key')}:{' '}
            <code className='bg-muted rounded px-1 py-0.5'>{OPTION_KEY}</code>
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Use the optional vendor field to partition video models by provider while keeping the model-key JSON structure.'
            )}
          </p>
        </AlertDescription>
      </Alert>

      <section className='space-y-2'>
        <div>
          <h3 className='text-sm font-medium'>{t('Vendor partitions')}</h3>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Sora, Veo, Seedance, Kling and other video models can keep separate rate-card groups.'
            )}
          </p>
        </div>
        <div className='flex flex-wrap gap-2 text-xs'>
          {Object.entries(VENDOR_LABELS)
            .filter(([key]) => key !== 'unclassified')
            .map(([key, label]) => (
              <span
                key={key}
                className='inline-flex items-center gap-1 rounded-md border px-2 py-1'
              >
                <code>vendor: "{key}"</code>
                <span className='text-muted-foreground'>{t(label)}</span>
              </span>
            ))}
        </div>
        {vendorSummary.length > 0 ? (
          <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
            {vendorSummary.map((vendor) => (
              <div
                key={vendor.key}
                className='bg-background rounded-md border p-3'
              >
                <div className='flex items-center justify-between gap-2'>
                  <div className='truncate text-sm font-medium'>
                    {t(vendor.label)}
                  </div>
                  <code className='bg-muted rounded px-1 py-0.5 text-xs'>
                    {vendor.key}
                  </code>
                </div>
                <div className='text-muted-foreground mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs'>
                  <span>
                    {vendor.modelCount} {t('cards')}
                  </span>
                  <span>
                    {vendor.rowCount} {t('rows')}
                  </span>
                </div>
                <div className='mt-2 space-y-1'>
                  {vendor.models.slice(0, 3).map((model) => (
                    <div
                      key={model}
                      className='text-muted-foreground truncate font-mono text-xs'
                    >
                      {model}
                    </div>
                  ))}
                  {vendor.models.length > 3 && (
                    <div className='text-muted-foreground text-xs'>
                      {t('{{count}} more', {
                        count: vendor.models.length - 3,
                      })}
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className='text-muted-foreground rounded-md border border-dashed p-3 text-sm'>
            {t('No task rate cards configured yet.')}
          </div>
        )}
      </section>

      <section className='space-y-2'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div>
            <h3 className='text-sm font-medium'>
              {t('Kling billing example')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Includes duration, quality, audio, and video-input pricing conditions.'
              )}
            </p>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <CopyButton
              value={KLING_RATE_CARD_EXAMPLE}
              variant='outline'
              size='sm'
              iconClassName='mr-2 h-4 w-4'
            >
              <span>{t('Copy example')}</span>
            </CopyButton>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => handleUseExample(KLING_RATE_CARD_EXAMPLE)}
            >
              <FileJson className='mr-2 h-4 w-4' />
              {t('Use example')}
            </Button>
          </div>
        </div>
        <Textarea
          rows={12}
          value={KLING_RATE_CARD_EXAMPLE}
          readOnly
          spellCheck={false}
          className='bg-muted/30 font-mono text-xs'
        />
      </section>

      <section className='space-y-2'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div>
            <h3 className='text-sm font-medium'>
              {t('MiniMax billing example')}
            </h3>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Uses the unified billing_type and billing_config shape for output video, input video, images, and audio.'
              )}
            </p>
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            <CopyButton
              value={MINIMAX_RATE_CARD_EXAMPLE}
              variant='outline'
              size='sm'
              iconClassName='mr-2 h-4 w-4'
            >
              <span>{t('Copy example')}</span>
            </CopyButton>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => handleUseExample(MINIMAX_RATE_CARD_EXAMPLE)}
            >
              <FileJson className='mr-2 h-4 w-4' />
              {t('Use example')}
            </Button>
          </div>
        </div>
        <Textarea
          rows={18}
          value={MINIMAX_RATE_CARD_EXAMPLE}
          readOnly
          spellCheck={false}
          className='bg-muted/30 font-mono text-xs'
        />
      </section>

      <section className='space-y-2'>
        <h3 className='text-sm font-medium'>{t('Current rate card JSON')}</h3>
        <Textarea
          rows={18}
          value={text}
          onChange={(event) => handleChange(event.target.value)}
          spellCheck={false}
          className='font-mono text-sm'
        />
        {error && <p className='text-destructive text-sm'>{error}</p>}
      </section>

      <SettingsPageActionsPortal>
        <Button
          type='button'
          size='sm'
          onClick={handleSave}
          disabled={Boolean(error) || updateOption.isPending}
        >
          {updateOption.isPending ? t('Saving...') : t('Save task rate cards')}
        </Button>
      </SettingsPageActionsPortal>
    </div>
  )
})
