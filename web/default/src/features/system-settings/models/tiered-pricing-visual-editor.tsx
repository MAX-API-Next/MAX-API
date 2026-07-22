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
import { useEffect, useId, useMemo, useState } from 'react'
import { ChevronDown, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { BILLING_EXTRA_VARS } from '@/features/pricing/lib/billing-expr'
import {
  CACHE_MODE_GENERIC,
  CACHE_MODE_TIMED,
  type CacheMode,
  type TierConditionInput,
  type VisualConfig,
  type VisualTier,
  getTierCacheMode,
  normalizeVisualConfig,
  normalizeVisualTier,
} from '@/features/pricing/lib/tier-expr'
import { DraftNumberInput } from './tiered-pricing-fields'
import {
  formatTokenHint,
  priceToUnitCost,
  unitCostToPrice,
} from './tiered-pricing-utils'

const PRICE_SUFFIX_KEY = '$/1M tokens'
const CACHE_PRICE_VARS = BILLING_EXTRA_VARS.filter(
  (variable) => variable.group === 'cache'
)
const MEDIA_PRICE_VARS = BILLING_EXTRA_VARS.filter(
  (variable) => variable.group === 'media'
)

const CONDITION_INPUT_OPTIONS: {
  value: TierConditionInput['var']
  labelKey: string
}[] = [
  { value: 'len', labelKey: 'Full input length' },
  { value: 'p', labelKey: 'Billable input tokens' },
  { value: 'c', labelKey: 'Billable output tokens' },
]
const OPS: TierConditionInput['op'][] = ['<', '<=', '>', '>=']

// ---------------------------------------------------------------------------
// Tier condition row
// ---------------------------------------------------------------------------

type ConditionRowProps = {
  condition: TierConditionInput
  onChange: (next: TierConditionInput) => void
  onRemove: () => void
  removeDisabled?: boolean
}

function ConditionRow({
  condition,
  onChange,
  onRemove,
  removeDisabled,
}: ConditionRowProps) {
  const { t } = useTranslation()
  const currentInputOption = CONDITION_INPUT_OPTIONS.find(
    (option) => option.value === condition.var
  )

  return (
    <div className='flex items-center gap-2'>
      <Select
        items={[
          ...CONDITION_INPUT_OPTIONS.map((option) => ({
            value: option.value,
            label: t(option.labelKey),
          })),
        ]}
        value={condition.var}
        onValueChange={(value) =>
          onChange({ ...condition, var: value as TierConditionInput['var'] })
        }
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>
            {currentInputOption
              ? t(currentInputOption.labelKey)
              : condition.var}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {CONDITION_INPUT_OPTIONS.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {t(option.labelKey)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={[...OPS.map((op) => ({ value: op, label: op }))]}
        value={condition.op}
        onValueChange={(value) =>
          onChange({ ...condition, op: value as TierConditionInput['op'] })
        }
      >
        <SelectTrigger className='w-20' size='sm'>
          <SelectValue />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {OPS.map((op) => (
              <SelectItem key={op} value={op}>
                {op}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <DraftNumberInput
        min={0}
        value={condition.value}
        onValueChange={(value) => onChange({ ...condition, value })}
        aria-label={t('Condition Value')}
        placeholder={t('tokens')}
        className='w-32'
      />
      <span className='text-muted-foreground text-xs'>
        {formatTokenHint(condition.value, t)}
      </span>
      <Button
        type='button'
        variant='ghost'
        size='icon'
        onClick={onRemove}
        disabled={removeDisabled}
        aria-label={t('Remove')}
        className='ml-auto'
      >
        <Trash2 className='text-destructive h-4 w-4' />
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Price input field
// ---------------------------------------------------------------------------

type PriceFieldProps = {
  label: string
  hint?: string
  value: number
  onChange: (next: number) => void
}

function PriceField({ label, hint, value, onChange }: PriceFieldProps) {
  const inputId = useId()
  return (
    <div className='w-36 space-y-0.5'>
      <Label htmlFor={inputId} className='text-muted-foreground text-xs'>
        {label}
      </Label>
      <DraftNumberInput
        id={inputId}
        min={0}
        step={0.000001}
        value={Number.isFinite(value) ? value : 0}
        onValueChange={onChange}
        className='h-8 w-full'
      />
      {hint && <p className='text-muted-foreground text-xs'>{hint}</p>}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Single tier card (visual editor)
// ---------------------------------------------------------------------------

type VisualTierCardProps = {
  tier: VisualTier
  index: number
  total: number
  onChange: (next: VisualTier) => void
  onRemove: () => void
  onAddCondition: () => void
}

function VisualTierCard({
  tier,
  index,
  total,
  onChange,
  onRemove,
  onAddCondition,
}: VisualTierCardProps) {
  const { t } = useTranslation()
  const cacheMode = getTierCacheMode(tier)
  const isFallbackTier = index === total - 1

  const handleConditionChange = (
    conditionIndex: number,
    next: TierConditionInput
  ) => {
    const conditions = [...tier.conditions]
    conditions[conditionIndex] = next
    onChange({ ...tier, conditions })
  }

  const handleConditionRemove = (conditionIndex: number) => {
    if (!isFallbackTier && tier.conditions.length <= 1) return
    onChange({
      ...tier,
      conditions: tier.conditions.filter((_, i) => i !== conditionIndex),
    })
  }

  const handlePriceChange = (field: keyof VisualTier, value: number) => {
    onChange({ ...tier, [field]: value })
  }

  const handleCacheModeChange = (mode: CacheMode) => {
    onChange({
      ...tier,
      cache_mode: mode,
      cache_create_1h_unit_cost:
        mode === CACHE_MODE_TIMED ? (tier.cache_create_1h_unit_cost ?? 0) : 0,
    })
  }

  const inputUnitPrice = unitCostToPrice(tier.input_unit_cost)
  const outputUnitPrice = unitCostToPrice(tier.output_unit_cost)
  const hasMediaPricing = MEDIA_PRICE_VARS.some((variable) => {
    const fieldKey = variable.tierField as keyof VisualTier
    return unitCostToPrice((tier[fieldKey] as number | undefined) ?? 0) > 0
  })
  const [mediaOpen, setMediaOpen] = useState(hasMediaPricing)

  useEffect(() => {
    // Reveal newly configured media prices when controlled data changes.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (hasMediaPricing) setMediaOpen(true)
  }, [hasMediaPricing])

  const renderPriceVariable = (
    variable: (typeof BILLING_EXTRA_VARS)[number]
  ) => {
    const fieldKey = variable.tierField as keyof VisualTier
    const value = unitCostToPrice((tier[fieldKey] as number | undefined) ?? 0)

    return (
      <PriceField
        key={variable.key}
        label={t(variable.label)}
        value={value}
        onChange={(next) => handlePriceChange(fieldKey, priceToUnitCost(next))}
      />
    )
  }

  return (
    <div className='space-y-3 rounded-lg border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex items-center gap-2'>
          <Badge variant='outline'>
            {t('Tier')} {index + 1} / {total}
          </Badge>
          {tier.conditions.length === 0 && (
            <Badge variant='secondary'>{t('Fallback tier')}</Badge>
          )}
          <Input
            value={tier.label}
            onChange={(event) =>
              onChange({ ...tier, label: event.target.value })
            }
            aria-label={t('Tier name')}
            placeholder={t('Tier name')}
            className='h-7 w-36'
          />
        </div>
        <Button
          type='button'
          variant='ghost'
          size='icon'
          onClick={onRemove}
          disabled={total <= 1 || isFallbackTier}
          aria-label={t('Remove tier')}
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>

      {/* Conditions */}
      <div className='space-y-1.5'>
        <div className='flex h-7 items-center justify-between'>
          <Label className='text-xs font-medium'>{t('Tier conditions')}</Label>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={onAddCondition}
            disabled={isFallbackTier || tier.conditions.length >= 2}
            className='h-7 px-2 text-xs'
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add condition')}
          </Button>
        </div>
        {tier.conditions.length === 0 ? (
          <p className='text-muted-foreground text-xs'>
            {t('Always matches (default tier).')}
          </p>
        ) : (
          tier.conditions.map((condition, conditionIndex) => (
            <ConditionRow
              key={conditionIndex}
              condition={condition}
              onChange={(next) => handleConditionChange(conditionIndex, next)}
              onRemove={() => handleConditionRemove(conditionIndex)}
              removeDisabled={!isFallbackTier && tier.conditions.length <= 1}
            />
          ))
        )}
      </div>

      <div className='space-y-2'>
        <div className='flex items-center justify-between gap-3'>
          <Label className='text-sm font-semibold'>{t('Token prices')}</Label>
          <span className='bg-muted text-muted-foreground rounded-md px-2 py-1 text-xs'>
            {t(PRICE_SUFFIX_KEY)}
          </span>
        </div>

        <div className='space-y-3'>
          <div className='flex flex-wrap gap-x-4 gap-y-2'>
            <PriceField
              label={t('Input price')}
              value={inputUnitPrice}
              onChange={(value) =>
                handlePriceChange('input_unit_cost', priceToUnitCost(value))
              }
            />
            <PriceField
              label={t('Output price')}
              value={outputUnitPrice}
              onChange={(value) =>
                handlePriceChange('output_unit_cost', priceToUnitCost(value))
              }
            />
          </div>

          <div className='space-y-2'>
            <div className='flex h-7 items-center'>
              <Tabs
                value={cacheMode}
                onValueChange={(value) =>
                  value !== null && handleCacheModeChange(value as CacheMode)
                }
              >
                <TabsList className='h-8'>
                  <TabsTrigger
                    value={CACHE_MODE_GENERIC}
                    className='px-2 text-xs'
                  >
                    {t('Generic cache')}
                  </TabsTrigger>
                  <TabsTrigger
                    value={CACHE_MODE_TIMED}
                    className='px-2 text-xs'
                  >
                    {t('Time-sliced cache (Claude)')}
                  </TabsTrigger>
                </TabsList>
              </Tabs>
            </div>
            <div className='flex flex-wrap gap-x-4 gap-y-2'>
              {CACHE_PRICE_VARS.map((variable) => {
                if (variable.key === 'cc1h' && cacheMode !== CACHE_MODE_TIMED) {
                  return null
                }
                return renderPriceVariable(variable)
              })}
            </div>
          </div>
        </div>
      </div>

      {/* Media prices */}
      <div className='space-y-1.5'>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='h-7 px-2 text-xs'
          onClick={() => setMediaOpen((prev) => !prev)}
        >
          <ChevronDown
            className={cn(
              'mr-1 h-3 w-3 transition-transform',
              mediaOpen && 'rotate-180'
            )}
          />
          {t('Media pricing')}
        </Button>
        {mediaOpen && (
          <div className='flex flex-wrap gap-x-4 gap-y-2'>
            {MEDIA_PRICE_VARS.map(renderPriceVariable)}
          </div>
        )}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Visual editor (list of tiers)
// ---------------------------------------------------------------------------

type VisualEditorProps = {
  visualConfig: VisualConfig | null
  onChange: (next: VisualConfig) => void
}

export function VisualEditor({ visualConfig, onChange }: VisualEditorProps) {
  const { t } = useTranslation()
  const config = useMemo(
    () => normalizeVisualConfig(visualConfig),
    [visualConfig]
  )

  const handleTierChange = (index: number, next: VisualTier) => {
    const tiers = [...config.tiers]
    const lastIndex = tiers.length - 1
    const normalized = normalizeVisualTier(
      index === lastIndex ? { ...next, conditions: [] } : next
    )
    if (index < lastIndex && normalized.conditions.length === 0) return
    tiers[index] = normalized
    onChange({ ...config, tiers })
  }

  const handleAddTier = () => {
    const tiers = [...config.tiers]
    const lastIndex = tiers.length - 1
    // When adding a new fallback, give the previous catch-all tier a default
    // upper-bound condition so the expression compiles into a sane two-tier
    // shape. Mirrors the legacy editor UX for adding tiers.
    if (lastIndex >= 0 && tiers[lastIndex].conditions.length === 0) {
      tiers[lastIndex] = normalizeVisualTier({
        ...tiers[lastIndex],
        conditions: [{ var: 'len', op: '<', value: 200000 }],
      })
    }
    tiers.push(
      normalizeVisualTier({
        label: `tier_${tiers.length + 1}`,
        conditions: [],
        input_unit_cost: 0,
        output_unit_cost: 0,
      })
    )
    onChange({ ...config, tiers })
  }

  const handleRemoveTier = (index: number) => {
    if (index === config.tiers.length - 1) return
    const tiers = config.tiers.filter((_, i) => i !== index)
    onChange({ ...config, tiers: tiers.length > 0 ? tiers : config.tiers })
  }

  const handleAddCondition = (index: number) => {
    const tier = config.tiers[index]
    if (index === config.tiers.length - 1) return
    if (tier.conditions.length >= 2) return
    // Prefer `len` (input length) over `p`/`c` for tier conditions because
    // `p` is subject to auto-exclusion when sub-categories like `cr` are
    // priced separately, which can misroute long-input requests into shorter
    // tiers when cache-hits reduce the effective `p`.
    const usedVars = new Set(tier.conditions.map((c) => c.var))
    const nextVar: TierConditionInput['var'] = usedVars.has('len') ? 'c' : 'len'
    onChange({
      ...config,
      tiers: config.tiers.map((current, i) =>
        i === index
          ? {
              ...current,
              conditions: [
                ...tier.conditions,
                { var: nextVar, op: '<', value: 200000 },
              ],
            }
          : current
      ),
    })
  }

  return (
    <div className='space-y-2'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Each tier supports up to 2 conditions. The last tier without conditions is the fallback.'
        )}
      </p>
      {config.tiers.map((tier, index) => (
        <VisualTierCard
          key={index}
          tier={tier}
          index={index}
          total={config.tiers.length}
          onChange={(next) => handleTierChange(index, next)}
          onRemove={() => handleRemoveTier(index)}
          onAddCondition={() => handleAddCondition(index)}
        />
      ))}
      <Button
        type='button'
        variant='outline'
        size='sm'
        className='h-9 w-36 justify-center'
        onClick={handleAddTier}
      >
        <Plus className='mr-2 h-4 w-4' />
        {t('Add tier')}
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Raw expression editor
// ---------------------------------------------------------------------------

type RawExprEditorProps = {
  exprString: string
  onChange: (value: string) => void
}

export function RawExprEditor({ exprString, onChange }: RawExprEditorProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-3'>
      <Alert>
        <AlertDescription className='space-y-1 text-xs'>
          <div>
            {t('Variables')}: <code>len</code>, <code>p</code>, <code>c</code>,{' '}
            <code>cr</code>, <code>cc</code>, <code>cc1h</code>,{' '}
            <code>img</code>, <code>img_o</code>, <code>ai</code>,{' '}
            <code>ao</code>
          </div>
          <div>
            {t('Functions')}: <code>tier(name, value)</code>, <code>max</code>,{' '}
            <code>min</code>, <code>ceil</code>, <code>floor</code>,{' '}
            <code>abs</code>, <code>header(name)</code>,{' '}
            <code>param(path)</code>, <code>has(source, text)</code>
          </div>
        </AlertDescription>
      </Alert>
      <Textarea
        value={exprString}
        onChange={(event) => onChange(event.target.value)}
        placeholder='tier("base", p * 3 + c * 15)'
        rows={6}
        className='font-mono text-xs'
        spellCheck={false}
      />
    </div>
  )
}
