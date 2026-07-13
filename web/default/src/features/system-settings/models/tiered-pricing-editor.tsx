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
import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
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
import {
  BILLING_EXTRA_VARS,
  COMMON_TIMEZONES,
  MATCH_CONTAINS,
  MATCH_EQ,
  MATCH_EXISTS,
  MATCH_GT,
  MATCH_GTE,
  MATCH_LT,
  MATCH_LTE,
  MATCH_RANGE,
  SOURCE_HEADER,
  SOURCE_PARAM,
  SOURCE_TIME,
  TIME_FUNCS,
  buildRequestRuleExpr,
  combineBillingExpr,
  createEmptyCondition,
  createEmptyRuleGroup,
  createEmptyTimeCondition,
  getRequestRuleMatchOptions,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
  type ParamHeaderCondition,
  type RequestCondition,
  type RequestRuleGroup,
  type TimeCondition,
  type TimeFunc,
} from '@/features/pricing/lib/billing-expr'
import {
  CACHE_MODE_GENERIC,
  CACHE_MODE_TIMED,
  type CacheMode,
  type TierConditionInput,
  type VisualConfig,
  type VisualTier,
  createDefaultVisualConfig,
  generateExprFromVisualConfig,
  getTierCacheMode,
  normalizeVisualConfig,
  normalizeVisualTier,
  tryParseVisualConfig,
} from '@/features/pricing/lib/tier-expr'
import { DraftNumberInput } from './tiered-pricing-fields'
import {
  CostEstimator,
  LlmPromptHelper,
  PresetSection,
  type Preset,
} from './tiered-pricing-support'
import {
  formatTokenHint,
  priceToUnitCost,
  unitCostToPrice,
} from './tiered-pricing-utils'

const PRICE_SUFFIX = '$/1M tokens'
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
}

function ConditionRow({ condition, onChange, onRemove }: ConditionRowProps) {
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
        placeholder='tokens'
        className='w-32'
      />
      <span className='text-muted-foreground text-xs'>
        {formatTokenHint(condition.value)}
      </span>
      <Button
        variant='ghost'
        size='icon'
        onClick={onRemove}
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
  return (
    <div className='w-36 space-y-0.5'>
      <Label className='text-muted-foreground text-xs'>{label}</Label>
      <DraftNumberInput
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

  const handleConditionChange = (
    conditionIndex: number,
    next: TierConditionInput
  ) => {
    const conditions = [...tier.conditions]
    conditions[conditionIndex] = next
    onChange({ ...tier, conditions })
  }

  const handleConditionRemove = (conditionIndex: number) => {
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
            placeholder={t('Tier name')}
            className='h-7 w-36'
          />
        </div>
        <Button
          variant='ghost'
          size='icon'
          onClick={onRemove}
          disabled={total <= 1}
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
            variant='ghost'
            size='sm'
            onClick={onAddCondition}
            disabled={tier.conditions.length >= 2}
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
            />
          ))
        )}
      </div>

      <div className='space-y-2'>
        <div className='flex items-center justify-between gap-3'>
          <Label className='text-sm font-semibold'>{t('Token prices')}</Label>
          <span className='bg-muted text-muted-foreground rounded-md px-2 py-1 text-xs'>
            {PRICE_SUFFIX}
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

function VisualEditor({ visualConfig, onChange }: VisualEditorProps) {
  const { t } = useTranslation()
  const config = useMemo(
    () => normalizeVisualConfig(visualConfig),
    [visualConfig]
  )

  const handleTierChange = (index: number, next: VisualTier) => {
    const tiers = [...config.tiers]
    tiers[index] = normalizeVisualTier(next)
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
    const tiers = config.tiers.filter((_, i) => i !== index)
    onChange({ ...config, tiers: tiers.length > 0 ? tiers : config.tiers })
  }

  const handleAddCondition = (index: number) => {
    const tier = config.tiers[index]
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

function RawExprEditor({ exprString, onChange }: RawExprEditorProps) {
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

// ---------------------------------------------------------------------------
// Request rule condition row
// ---------------------------------------------------------------------------

type RuleConditionRowProps = {
  condition: RequestCondition
  onChange: (next: RequestCondition) => void
  onRemove: () => void
}

function RuleConditionRow({
  condition,
  onChange,
  onRemove,
}: RuleConditionRowProps) {
  const { t } = useTranslation()
  const matchOptions = getRequestRuleMatchOptions(condition.source)
  const getMatchLabel = (mode: string) => {
    switch (mode) {
      case MATCH_EQ:
        return t('Equals')
      case MATCH_CONTAINS:
        return t('Contains')
      case MATCH_EXISTS:
        return t('Exists')
      case MATCH_GT:
        return t('Greater than')
      case MATCH_GTE:
        return t('Greater than or equal')
      case MATCH_LT:
        return t('Less than')
      case MATCH_LTE:
        return t('Less than or equal')
      case MATCH_RANGE:
        return t('Overnight range')
      default:
        return mode
    }
  }
  const getTimeFuncLabel = (timeFunc: TimeFunc) => {
    switch (timeFunc) {
      case 'hour':
        return t('Hour of day')
      case 'minute':
        return t('Minute')
      case 'weekday':
        return t('Weekday')
      case 'month':
        return t('Month number')
      case 'day':
        return t('Day of month')
      default:
        return timeFunc
    }
  }
  const sourceLabel =
    condition.source === SOURCE_PARAM
      ? t('Body param')
      : condition.source === SOURCE_HEADER
        ? t('Header')
        : t('Time')

  const handleSourceChange = (source: string) => {
    if (source === SOURCE_TIME) {
      onChange(createEmptyTimeCondition())
    } else if (source === SOURCE_HEADER || source === SOURCE_PARAM) {
      onChange({
        ...createEmptyCondition(),
        source: source as 'param' | 'header',
      })
    }
  }

  const handleModeChange = (mode: string) => {
    onChange({ ...condition, mode } as RequestCondition)
  }

  const renderTimeCondition = (timeCond: TimeCondition) => (
    <>
      <Select
        items={[
          ...TIME_FUNCS.map((fn) => ({
            value: fn,
            label: getTimeFuncLabel(fn),
          })),
        ]}
        value={timeCond.timeFunc}
        onValueChange={(value) =>
          onChange({ ...timeCond, timeFunc: value as TimeFunc })
        }
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getTimeFuncLabel(timeCond.timeFunc)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {TIME_FUNCS.map((fn) => (
              <SelectItem key={fn} value={fn}>
                {getTimeFuncLabel(fn)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={[
          ...COMMON_TIMEZONES.map((tz) => ({
            value: tz.value,
            label: tz.label,
          })),
        ]}
        value={timeCond.timezone}
        onValueChange={(value) =>
          value !== null && onChange({ ...timeCond, timezone: value })
        }
      >
        <SelectTrigger className='w-56' size='sm'>
          <SelectValue>
            {COMMON_TIMEZONES.find((tz) => tz.value === timeCond.timezone)
              ?.label ?? timeCond.timezone}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {COMMON_TIMEZONES.map((tz) => (
              <SelectItem key={tz.value} value={tz.value}>
                {tz.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={[
          ...matchOptions.map((option) => ({
            value: option.value,
            label: getMatchLabel(option.value),
          })),
        ]}
        value={timeCond.mode}
        onValueChange={(v) => v !== null && handleModeChange(v)}
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getMatchLabel(timeCond.mode)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {matchOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {getMatchLabel(option.value)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      {timeCond.mode === MATCH_RANGE ? (
        <>
          <DraftNumberInput
            value={timeCond.rangeStart}
            onValueChange={(value) =>
              onChange({ ...timeCond, rangeStart: String(value) })
            }
            placeholder={t('Start')}
            className='w-20'
          />
          <span className='text-muted-foreground text-xs'>~</span>
          <DraftNumberInput
            value={timeCond.rangeEnd}
            onValueChange={(value) =>
              onChange({ ...timeCond, rangeEnd: String(value) })
            }
            placeholder={t('End')}
            className='w-20'
          />
        </>
      ) : (
        <DraftNumberInput
          value={timeCond.value}
          onValueChange={(value) =>
            onChange({ ...timeCond, value: String(value) })
          }
          placeholder={t('Value')}
          className='w-24'
        />
      )}
    </>
  )

  const renderParamHeaderCondition = (phCond: ParamHeaderCondition) => (
    <>
      <Input
        value={phCond.path}
        onChange={(event) => onChange({ ...phCond, path: event.target.value })}
        placeholder={
          phCond.source === SOURCE_HEADER ? 'X-Header-Name' : 'service_tier'
        }
        className='w-44'
      />
      <Select
        items={[
          ...matchOptions.map((option) => ({
            value: option.value,
            label: getMatchLabel(option.value),
          })),
        ]}
        value={phCond.mode}
        onValueChange={(v) => v !== null && handleModeChange(v)}
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getMatchLabel(phCond.mode)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {matchOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {getMatchLabel(option.value)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      {phCond.mode !== MATCH_EXISTS && (
        <Input
          value={phCond.value}
          onChange={(event) =>
            onChange({ ...phCond, value: event.target.value })
          }
          placeholder={t('Value')}
          className='w-44'
        />
      )}
    </>
  )

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <Select
        items={[
          { value: SOURCE_PARAM, label: t('Body param') },
          { value: SOURCE_HEADER, label: t('Header') },
          { value: SOURCE_TIME, label: t('Time') },
        ]}
        value={condition.source}
        onValueChange={(v) => v !== null && handleSourceChange(v)}
      >
        <SelectTrigger className='w-28' size='sm'>
          <SelectValue>{sourceLabel}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            <SelectItem value={SOURCE_PARAM}>{t('Body param')}</SelectItem>
            <SelectItem value={SOURCE_HEADER}>{t('Header')}</SelectItem>
            <SelectItem value={SOURCE_TIME}>{t('Time')}</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
      {condition.source === SOURCE_TIME
        ? renderTimeCondition(condition as TimeCondition)
        : renderParamHeaderCondition(condition as ParamHeaderCondition)}
      <Button
        variant='ghost'
        size='icon'
        onClick={onRemove}
        aria-label={t('Remove condition')}
        className='ml-auto'
      >
        <Trash2 className='text-destructive h-4 w-4' />
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Request rule group card
// ---------------------------------------------------------------------------

type RuleGroupCardProps = {
  group: RequestRuleGroup
  index: number
  onChange: (next: RequestRuleGroup) => void
  onRemove: () => void
}

function RuleGroupCard({
  group,
  index,
  onChange,
  onRemove,
}: RuleGroupCardProps) {
  const { t } = useTranslation()

  const handleConditionChange = (
    conditionIndex: number,
    next: RequestCondition
  ) => {
    const conditions = [...group.conditions]
    conditions[conditionIndex] = next
    onChange({ ...group, conditions })
  }

  const handleAddCondition = (timeMode: boolean) => {
    onChange({
      ...group,
      conditions: [
        ...group.conditions,
        timeMode ? createEmptyTimeCondition() : createEmptyCondition(),
      ],
    })
  }

  return (
    <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
      <div className='flex items-center justify-between gap-2'>
        <Badge variant='outline'>
          {t('Rule group')} #{index + 1}
        </Badge>
        <Button
          variant='ghost'
          size='icon'
          onClick={onRemove}
          aria-label={t('Remove rule group')}
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>

      <div className='space-y-2'>
        {group.conditions.map((condition, conditionIndex) => (
          <RuleConditionRow
            key={conditionIndex}
            condition={condition}
            onChange={(next) => handleConditionChange(conditionIndex, next)}
            onRemove={() =>
              onChange({
                ...group,
                conditions: group.conditions.filter(
                  (_, i) => i !== conditionIndex
                ),
              })
            }
          />
        ))}
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => handleAddCondition(false)}
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add param/header')}
          </Button>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => handleAddCondition(true)}
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add time condition')}
          </Button>
        </div>
      </div>

      <div className='flex items-center gap-2'>
        <Label className='text-xs'>{t('Multiplier')}</Label>
        <DraftNumberInput
          min={0}
          step={0.000001}
          value={group.multiplier}
          onValueChange={(value) =>
            onChange({ ...group, multiplier: String(value) })
          }
          className='w-32'
          placeholder='1.0'
        />
        <span className='text-muted-foreground text-xs'>
          {t('Final cost = base × multiplier when conditions match')}
        </span>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Main editor
// ---------------------------------------------------------------------------

export type TieredPricingEditorProps = {
  modelName?: string
  billingExpr: string
  requestRuleExpr: string
  onBillingExprChange: (next: string) => void
  onRequestRuleExprChange: (next: string) => void
}

type EditorMode = 'visual' | 'raw'

export const TieredPricingEditor = memo(function TieredPricingEditor({
  modelName,
  billingExpr: currentExpr,
  requestRuleExpr: currentRequestRuleExpr,
  onBillingExprChange,
  onRequestRuleExprChange,
}: TieredPricingEditorProps) {
  const { t } = useTranslation()
  const [editorMode, setEditorMode] = useState<EditorMode>('visual')
  const [visualConfig, setVisualConfig] = useState<VisualConfig | null>(() =>
    tryParseVisualConfig(currentExpr)
  )
  const [rawExpr, setRawExpr] = useState(() =>
    combineBillingExpr(currentExpr || '', currentRequestRuleExpr || '')
  )
  const [requestRuleGroups, setRequestRuleGroups] = useState<
    RequestRuleGroup[]
  >(() => tryParseRequestRuleExpr(currentRequestRuleExpr) || [])
  const initRef = useRef(false)

  useEffect(() => {
    if (initRef.current) return
    initRef.current = true
    const parsedConfig = tryParseVisualConfig(currentExpr)
    if (parsedConfig) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setVisualConfig(parsedConfig)
      setEditorMode('visual')
    } else if (currentExpr) {
      setVisualConfig(null)
      setEditorMode('raw')
    } else {
      setVisualConfig(createDefaultVisualConfig())
    }
    setRawExpr(
      combineBillingExpr(currentExpr || '', currentRequestRuleExpr || '')
    )
    setRequestRuleGroups(tryParseRequestRuleExpr(currentRequestRuleExpr) || [])
  }, [currentExpr, currentRequestRuleExpr])

  useEffect(() => {
    initRef.current = false
  }, [modelName])

  const canUseVisualRules = useMemo(() => {
    if (!currentRequestRuleExpr) return true
    return tryParseRequestRuleExpr(currentRequestRuleExpr) !== null
  }, [currentRequestRuleExpr])

  const effectiveExpr = useMemo(() => {
    if (editorMode === 'visual') {
      return generateExprFromVisualConfig(visualConfig)
    }
    const { billingExpr } = splitBillingExprAndRequestRules(rawExpr)
    return billingExpr
  }, [editorMode, visualConfig, rawExpr])

  useEffect(() => {
    if (effectiveExpr !== currentExpr) {
      onBillingExprChange(effectiveExpr)
    }
  }, [effectiveExpr, currentExpr, onBillingExprChange])

  useEffect(() => {
    if (editorMode !== 'visual') return
    const ruleExpr = buildRequestRuleExpr(requestRuleGroups)
    if (ruleExpr !== currentRequestRuleExpr) {
      onRequestRuleExprChange(ruleExpr)
    }
  }, [
    editorMode,
    requestRuleGroups,
    currentRequestRuleExpr,
    onRequestRuleExprChange,
  ])

  const handleVisualChange = useCallback((next: VisualConfig) => {
    setVisualConfig(next)
  }, [])

  const handleRawChange = useCallback(
    (value: string) => {
      setRawExpr(value)
      const { requestRuleExpr: ruleStr } =
        splitBillingExprAndRequestRules(value)
      onRequestRuleExprChange(ruleStr)
    },
    [onRequestRuleExprChange]
  )

  const handleModeChange = useCallback(
    (next: EditorMode) => {
      if (next === 'visual') {
        const { billingExpr, requestRuleExpr: ruleStr } =
          splitBillingExprAndRequestRules(rawExpr)
        const parsed = tryParseVisualConfig(billingExpr)
        if (parsed) {
          setVisualConfig(parsed)
        } else {
          setVisualConfig(createDefaultVisualConfig())
        }
        const parsedGroups = tryParseRequestRuleExpr(ruleStr)
        setRequestRuleGroups(parsedGroups || [])
        onRequestRuleExprChange(ruleStr)
      } else {
        const expr = generateExprFromVisualConfig(visualConfig)
        const ruleExpr = buildRequestRuleExpr(requestRuleGroups)
        setRawExpr(combineBillingExpr(expr, ruleExpr) || expr)
      }
      setEditorMode(next)
    },
    [rawExpr, visualConfig, requestRuleGroups, onRequestRuleExprChange]
  )

  const applyPreset = useCallback(
    (preset: Preset) => {
      const presetGroups = preset.requestRules || []
      const ruleExpr = buildRequestRuleExpr(presetGroups)
      const combined = combineBillingExpr(preset.expr, ruleExpr) || preset.expr
      setRawExpr(combined)
      const parsed = tryParseVisualConfig(preset.expr)
      if (parsed) {
        setVisualConfig(parsed)
        setEditorMode('visual')
      } else {
        setEditorMode('raw')
        setVisualConfig(null)
      }
      setRequestRuleGroups(presetGroups)
      onRequestRuleExprChange(ruleExpr)
    },
    [onRequestRuleExprChange]
  )

  const handleRuleGroupsChange = useCallback((next: RequestRuleGroup[]) => {
    setRequestRuleGroups(next)
  }, [])

  return (
    <div className='space-y-4'>
      <div className='flex items-center justify-between gap-2'>
        <Label className='text-xs'>{t('Editor mode')}</Label>
        <Select
          items={[
            { value: 'visual', label: t('Visual editor') },
            { value: 'raw', label: t('Expression editor') },
          ]}
          value={editorMode}
          onValueChange={(value) => handleModeChange(value as EditorMode)}
        >
          <SelectTrigger className='w-44' size='sm'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              <SelectItem value='visual'>{t('Visual editor')}</SelectItem>
              <SelectItem value='raw'>{t('Expression editor')}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>

      <div className='flex flex-wrap items-start gap-x-4 gap-y-1'>
        <div className='flex-1'>
          <PresetSection applyPreset={applyPreset} />
        </div>
        {editorMode === 'raw' && <LlmPromptHelper modelName={modelName} />}
      </div>

      <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
        {editorMode === 'visual' ? (
          <VisualEditor
            visualConfig={visualConfig}
            onChange={handleVisualChange}
          />
        ) : (
          <RawExprEditor exprString={rawExpr} onChange={handleRawChange} />
        )}

        {editorMode === 'visual' && (
          <div className='space-y-3 border-t pt-3'>
            <div className='space-y-1'>
              <h4 className='text-sm font-medium'>
                {t('Request rule pricing')}
              </h4>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'When conditions match, the final price is multiplied by X. Multiple matches multiply together; values < 1 act as discounts.'
                )}
              </p>
            </div>

            {currentRequestRuleExpr && !canUseVisualRules ? (
              <Alert>
                <AlertDescription className='text-xs'>
                  {t(
                    'This expression is too complex for the visual editor. Please switch to expression mode to edit.'
                  )}
                </AlertDescription>
              </Alert>
            ) : (
              <>
                {requestRuleGroups.map((group, groupIndex) => (
                  <RuleGroupCard
                    key={groupIndex}
                    group={group}
                    index={groupIndex}
                    onChange={(next) => {
                      const updated = [...requestRuleGroups]
                      updated[groupIndex] = next
                      handleRuleGroupsChange(updated)
                    }}
                    onRemove={() =>
                      handleRuleGroupsChange(
                        requestRuleGroups.filter((_, i) => i !== groupIndex)
                      )
                    }
                  />
                ))}
                <Button
                  variant='outline'
                  size='sm'
                  className='h-9 w-36 justify-center'
                  onClick={() =>
                    handleRuleGroupsChange([
                      ...requestRuleGroups,
                      createEmptyRuleGroup(),
                    ])
                  }
                >
                  <Plus className='mr-2 h-4 w-4' />
                  {t('Add rule group')}
                </Button>
              </>
            )}
          </div>
        )}
      </div>

      <CostEstimator effectiveExpr={effectiveExpr} />
    </div>
  )
})
