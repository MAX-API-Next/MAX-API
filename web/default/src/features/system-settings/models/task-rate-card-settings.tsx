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
import {
  memo,
  type ChangeEvent,
  type ReactElement,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from 'react'
import { Download, FileJson, Search, Upload } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
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

function downloadJson(filename: string, value: string): void {
  const blob = new Blob([value], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  setTimeout(() => URL.revokeObjectURL(url), 0)
}

type TaskRateCardSettingsProps = {
  defaultValue: string
}

type VendorSummary = {
  key: string
  label: string
  modelCount: number
  rowCount: number
  models: string[]
  modelRows: Record<string, number>
}

type BillingExampleSectionProps = {
  title: string
  description: string
  value: string
  rows: number
  onUseExample: () => void
}

const BillingExampleSection = memo(function BillingExampleSection(
  props: BillingExampleSectionProps
): ReactElement {
  const { t } = useTranslation()
  const headingId = useId()

  return (
    <section className='space-y-2'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div>
          <h3 id={headingId} className='text-sm font-medium'>
            {t(props.title)}
          </h3>
          <p className='text-muted-foreground text-xs'>
            {t(props.description)}
          </p>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <CopyButton
            value={props.value}
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
            onClick={props.onUseExample}
          >
            <FileJson className='mr-2 h-4 w-4' />
            {t('Use example')}
          </Button>
        </div>
      </div>
      <Textarea
        aria-labelledby={headingId}
        rows={props.rows}
        value={props.value}
        readOnly
        spellCheck={false}
        className='bg-muted/30 font-mono text-xs'
      />
    </section>
  )
})

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
      modelRows: {},
    }

    group.modelCount += 1
    group.rowCount += rows
    group.models.push(model)
    group.modelRows[model] = rows
    groups.set(vendor, group)
  }

  return Array.from(groups.values()).sort(sortVendors)
}

export const TaskRateCardSettings = memo(function TaskRateCardSettings({
  defaultValue,
}: TaskRateCardSettingsProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const currentRateCardHeadingId = useId()
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [search, setSearch] = useState('')
  const [vendorFilter, setVendorFilter] = useState('all')
  const fileInputRef = useRef<HTMLInputElement>(null)
  const editRevision = useRef(0)
  const initialText = useMemo(
    (): string => formatJsonForTextarea(defaultValue || '{}'),
    [defaultValue]
  )
  const vendorSummary = useMemo(() => buildVendorSummary(text), [text])
  useEffect((): void => {
    if (
      vendorFilter !== 'all' &&
      !vendorSummary.some(
        (vendor: VendorSummary): boolean => vendor.key === vendorFilter
      )
    ) {
      setVendorFilter('all')
    }
  }, [vendorFilter, vendorSummary])
  const filteredVendorSummary = useMemo((): VendorSummary[] => {
    const query = search.trim().toLowerCase()
    return vendorSummary
      .filter(
        (vendor: VendorSummary): boolean =>
          vendorFilter === 'all' || vendor.key === vendorFilter
      )
      .map((vendor: VendorSummary): VendorSummary => {
        if (!query) return vendor
        return {
          ...vendor,
          modelCount: vendor.models.filter((model: string): boolean =>
            model.toLowerCase().includes(query)
          ).length,
          rowCount: vendor.models
            .filter((model: string): boolean =>
              model.toLowerCase().includes(query)
            )
            .reduce(
              (sum: number, model: string): number =>
                sum + (vendor.modelRows[model] ?? 0),
              0
            ),
          models: vendor.models.filter((model: string): boolean =>
            model.toLowerCase().includes(query)
          ),
        }
      })
      .filter(
        (vendor: VendorSummary): boolean => !query || vendor.models.length > 0
      )
  }, [search, vendorFilter, vendorSummary])
  const isDirty = text !== initialText

  useEffect(() => {
    editRevision.current += 1
    setText(formatJsonForTextarea(defaultValue || '{}'))
    setError('')
    return () => {
      editRevision.current += 1
    }
  }, [defaultValue])

  const handleChange = useCallback(
    (value: string) => {
      editRevision.current += 1
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
    (example: string): void => {
      editRevision.current += 1
      setText(example)
      setError('')
      toast.success(t('Example loaded. Review prices before saving.'))
    },
    [t]
  )

  const handleUseKlingExample = useCallback((): void => {
    handleUseExample(KLING_RATE_CARD_EXAMPLE)
  }, [handleUseExample])

  const handleUseMiniMaxExample = useCallback((): void => {
    handleUseExample(MINIMAX_RATE_CARD_EXAMPLE)
  }, [handleUseExample])

  const handleImport = useCallback(
    async (event: ChangeEvent<HTMLInputElement>): Promise<void> => {
      const file = event.target.files?.[0]
      event.target.value = ''
      if (!file) return
      const revision = ++editRevision.current

      try {
        const contents = await file.text()
        if (revision !== editRevision.current) return
        const imported = formatJsonForTextarea(contents)
        if (!imported) throw new Error(t('Invalid JSON'))
        const parsed = JSON.parse(imported) as unknown
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          throw new Error(t('JSON must be an object'))
        }
        editRevision.current += 1
        setText(imported)
        setError('')
        toast.success(t('JSON imported. Review prices before saving.'))
      } catch (err) {
        if (revision !== editRevision.current) return
        const message = err instanceof Error ? err.message : t('Invalid JSON')
        toast.error(message)
      }
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
        <div className='flex flex-wrap items-center gap-2'>
          <div className='relative min-w-56 flex-1 sm:max-w-sm'>
            <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2' />
            <Input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('Search model name...')}
              aria-label={t('Search model name...')}
              className='pl-9'
            />
          </div>
          <div className='flex flex-wrap gap-1'>
            <Button
              type='button'
              size='sm'
              variant={vendorFilter === 'all' ? 'secondary' : 'ghost'}
              aria-pressed={vendorFilter === 'all'}
              onClick={() => setVendorFilter('all')}
            >
              {t('All')}
            </Button>
            {vendorSummary.map((vendor) => (
              <Button
                key={vendor.key}
                type='button'
                size='sm'
                variant={vendorFilter === vendor.key ? 'secondary' : 'ghost'}
                aria-pressed={vendorFilter === vendor.key}
                onClick={() => setVendorFilter(vendor.key)}
              >
                {t(vendor.label)}
              </Button>
            ))}
          </div>
          {isDirty && (
            <span className='text-muted-foreground text-xs'>
              {t('Unsaved changes')}
            </span>
          )}
        </div>
        {filteredVendorSummary.length > 0 ? (
          <div className='grid gap-3 md:grid-cols-2 xl:grid-cols-4'>
            {filteredVendorSummary.map((vendor) => (
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
            {vendorSummary.length > 0
              ? t('No matching task rate cards.')
              : t('No task rate cards configured yet.')}
          </div>
        )}
      </section>

      <BillingExampleSection
        title='Kling billing example'
        description='Includes duration, quality, audio, and video-input pricing conditions.'
        value={KLING_RATE_CARD_EXAMPLE}
        rows={12}
        onUseExample={handleUseKlingExample}
      />

      <BillingExampleSection
        title='MiniMax billing example'
        description='Uses the unified billing_type and billing_config shape for output video, input video, images, and audio.'
        value={MINIMAX_RATE_CARD_EXAMPLE}
        rows={18}
        onUseExample={handleUseMiniMaxExample}
      />

      <section className='space-y-2'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <h3 id={currentRateCardHeadingId} className='text-sm font-medium'>
            {t('Current rate card JSON')}
          </h3>
          <div className='flex flex-wrap items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => downloadJson('task-rate-cards.json', text || '{}')}
              disabled={Boolean(error)}
            >
              <Download className='mr-2 h-4 w-4' />
              {t('Download')}
            </Button>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() => fileInputRef.current?.click()}
            >
              <Upload className='mr-2 h-4 w-4' />
              {t('Upload file')}
            </Button>
            <input
              ref={fileInputRef}
              type='file'
              accept='application/json,.json'
              className='hidden'
              onChange={handleImport}
            />
          </div>
        </div>
        <Textarea
          aria-labelledby={currentRateCardHeadingId}
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
