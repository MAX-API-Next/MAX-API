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
import { useTranslation } from 'react-i18next'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  parseTiersFromExpr,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
  type ParsedTier,
  type RequestCondition,
} from '../lib/billing-expr'
import { getDynamicPriceEntries } from '../lib/dynamic-price'
import {
  type summarizeTierConditionGroup,
  summarizeTierConditionAlternatives,
  TIER_CONDITION_LABELS,
} from '../lib/tier-condition-display'
import type { TokenUnit } from '../types'

type ModelTierPricingProps = {
  billingExpr: string
  tokenUnit: TokenUnit
  priceRate: number
  usdExchangeRate: number
  showRechargePrice: boolean
}

function describeRequestCondition(
  condition: RequestCondition,
  t: (key: string) => string
): string {
  const comparison: Record<string, string> = {
    eq: '=',
    gt: '>',
    gte: '≥',
    lt: '<',
    lte: '≤',
  }
  if (condition.source === 'time') {
    const label = t(TIER_CONDITION_LABELS[condition.timeFunc])
    // The shared parser represents this form as an OR, including wraparound.
    // Do not infer an AND range merely from the order of its bounds.
    const value =
      condition.mode === 'range'
        ? `≥ ${condition.rangeStart} ${t('OR')} < ${condition.rangeEnd}`
        : `${comparison[condition.mode] ?? '='} ${condition.value}`
    return `${label} ${value} (${condition.timezone})`
  }
  const label = `${t(condition.source === 'header' ? 'Header' : 'Body param')} ${condition.path}`
  if (condition.mode === 'exists') return `${label} ${t('Exists')}`
  if (condition.mode === 'contains')
    return `${label} ${t('Contains')} ${condition.value}`
  return `${label} ${comparison[condition.mode] ?? '='} ${condition.value}`
}

function ConditionGroup(props: {
  summary: ReturnType<typeof summarizeTierConditionGroup>
}) {
  const { t } = useTranslation()
  const summary = props.summary
  const timezones = [
    ...new Set(
      summary.lines.flatMap((line) =>
        line.timezone === undefined ? [] : [line.timezone]
      )
    ),
  ]
  return (
    <div className='flex min-w-0 flex-col gap-1.5'>
      <ul className='text-muted-foreground flex flex-col gap-1 text-xs leading-relaxed'>
        {summary.lines.map((line, index) => (
          <li key={index} className='wrap-anywhere'>
            <span>{t(TIER_CONDITION_LABELS[line.variable])}</span>{' '}
            <span className='text-foreground tabular-nums'>{line.value}</span>
            {timezones.length > 1 &&
              line.timezone !== undefined &&
              ` (${line.timezone})`}
          </li>
        ))}
      </ul>
      {timezones.length === 1 && (
        <p className='text-muted-foreground text-xs wrap-anywhere'>
          {t('Timezone')}: {timezones[0]}
        </p>
      )}
      {summary.impossible && (
        <p className='text-destructive text-xs'>
          {t('These conditions cannot match together.')}
        </p>
      )}
    </div>
  )
}

function TierApplicability(props: { tiers: ParsedTier[] }) {
  const { t, i18n } = useTranslation()
  return (
    <section
      aria-label={t('Tier applicability')}
      className='flex min-w-0 flex-col gap-2'
    >
      <div className='flex flex-col gap-1'>
        <h4 className='text-sm font-medium'>{t('Tier applicability')}</h4>
        <p className='text-muted-foreground text-xs'>
          {t('Tiers are checked in order; the first match applies.')}
        </p>
      </div>
      <div className='grid min-w-0 grid-cols-1 gap-2 @min-[36rem]/tier-pricing:grid-cols-2'>
        {props.tiers.map((tier, index) => {
          const groups =
            tier.conditionGroups ??
            (tier.conditions.length ? [tier.conditions] : [])
          const summaries = summarizeTierConditionAlternatives(
            groups,
            i18n.language,
            t('OR')
          )
          return (
            <Card
              key={index}
              size='sm'
              className={cn(
                'min-w-0',
                groups.length ? 'bg-primary/5 ring-primary/15' : 'bg-muted/40'
              )}
            >
              <CardHeader>
                <CardTitle className='flex min-w-0 items-start gap-2'>
                  <Badge variant='outline'>{index + 1}</Badge>
                  <span className='min-w-0 wrap-anywhere'>
                    {tier.label || t('Default')}
                  </span>
                </CardTitle>
              </CardHeader>
              <CardContent className='flex min-w-0 flex-col gap-2'>
                {groups.length === 0 ? (
                  <p className='text-muted-foreground text-xs'>
                    {index === 0
                      ? t('Always matches (default tier).')
                      : t('When no earlier tier matches')}
                  </p>
                ) : (
                  <>
                    <p className='text-muted-foreground text-xs'>
                      {t('All conditions in a group must match.')}
                    </p>
                    {summaries.map((summary, groupIndex) => (
                      <div
                        key={groupIndex}
                        className='flex min-w-0 flex-col gap-2'
                      >
                        {groupIndex > 0 && (
                          <Badge variant='outline'>{t('OR')}</Badge>
                        )}
                        <ConditionGroup summary={summary} />
                      </div>
                    ))}
                  </>
                )}
              </CardContent>
            </Card>
          )
        })}
      </div>
    </section>
  )
}

/** Model-square-only presentation; the admin editor and historical logs stay separate. */
export function ModelTierPricing(props: ModelTierPricingProps) {
  const { t } = useTranslation()
  // Price formatters read the same config; subscribe so live currency changes rerender.
  useSystemConfigStore((state) => state.config.currency)
  const split = splitBillingExprAndRequestRules(props.billingExpr)
  const tiers = parseTiersFromExpr(split.billingExpr)
  const requestGroups = tryParseRequestRuleExpr(split.requestRuleExpr)
  const prices = tiers.map((tier) =>
    getDynamicPriceEntries(tier, { ...props, groupRatioMultiplier: 1 })
  )
  const fields = [
    ...new Map(prices.flat().map((entry) => [entry.field, entry])).values(),
  ]

  return (
    <section
      aria-label={t('Tiered price table')}
      className='@container/tier-pricing flex min-w-0 flex-col gap-4'
    >
      {tiers.length > 0 ? (
        <>
          <div className='flex min-w-0 flex-col gap-2'>
            <div className='flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1'>
              <h4 className='text-sm font-medium'>{t('Tiered price table')}</h4>
              <span className='text-muted-foreground text-xs'>
                {t('Prices shown per {{unit}} tokens', {
                  unit: props.tokenUnit === 'K' ? '1K' : '1M',
                })}
              </span>
            </div>
            <div className='hidden min-w-0 @min-[36rem]/tier-pricing:block'>
              <Table
                className={cn(
                  'table-fixed',
                  fields.length > 4 && 'min-w-[64rem]'
                )}
              >
                <TableHeader>
                  <TableRow>
                    <TableHead className='w-1/4 whitespace-normal'>
                      {t('Tier')}
                    </TableHead>
                    {fields.map((field) => (
                      <TableHead
                        key={field.field}
                        className='text-right whitespace-normal'
                      >
                        {t(field.shortLabel)}
                      </TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tiers.map((tier, index) => {
                    const entries = new Map(
                      prices[index].map((entry) => [entry.field, entry])
                    )
                    return (
                      <TableRow key={index}>
                        <TableCell className='whitespace-normal'>
                          <Badge
                            variant='secondary'
                            className='h-auto max-w-full wrap-anywhere whitespace-normal'
                          >
                            {tier.label || t('Default')}
                          </Badge>
                        </TableCell>
                        {fields.map((field) => (
                          <TableCell
                            key={field.field}
                            className='text-right font-mono tabular-nums'
                          >
                            {entries.get(field.field)?.formatted ?? '—'}
                          </TableCell>
                        ))}
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
            <div className='flex flex-col gap-3 @min-[36rem]/tier-pricing:hidden'>
              {tiers.map((tier, index) => {
                const entries = new Map(
                  prices[index].map((entry) => [entry.field, entry])
                )
                return (
                  <div
                    key={index}
                    className='flex min-w-0 flex-col gap-2 border-b pb-3 last:border-0 last:pb-0'
                  >
                    <Badge
                      variant='secondary'
                      className='h-auto max-w-full wrap-anywhere whitespace-normal'
                    >
                      {tier.label || t('Default')}
                    </Badge>
                    <dl className='grid grid-cols-2 gap-x-4 gap-y-2'>
                      {fields.map((field) => (
                        <div key={field.field} className='min-w-0'>
                          <dt className='text-muted-foreground text-xs wrap-anywhere'>
                            {t(field.shortLabel)}
                          </dt>
                          <dd className='font-mono text-sm wrap-anywhere tabular-nums'>
                            {entries.get(field.field)?.formatted ?? '—'}
                          </dd>
                        </div>
                      ))}
                    </dl>
                  </div>
                )
              })}
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Base tier prices, before group and request multipliers. A dash means no separate coefficient in this tier.'
              )}
            </p>
          </div>
          <TierApplicability tiers={tiers} />
        </>
      ) : (
        <p className='text-muted-foreground text-sm'>
          {t('Unable to parse structured pricing')}
        </p>
      )}
      {split.requestRuleExpr && (
        <div className='flex min-w-0 flex-col gap-2'>
          <h4 className='text-sm font-medium'>
            {t('Conditional multipliers')}
          </h4>
          <p className='text-muted-foreground text-xs'>
            {t(
              'When conditions match, the final price is multiplied by X. Multiple matches multiply together; values < 1 act as discounts.'
            )}
          </p>
          {requestGroups?.length ? (
            <ul className='flex flex-col gap-2'>
              {requestGroups.map((group, index) => (
                <li
                  key={index}
                  className='bg-muted/40 flex min-w-0 items-start justify-between gap-3 rounded-md p-3'
                >
                  <div className='flex min-w-0 flex-col gap-1'>
                    <p className='text-muted-foreground text-xs'>
                      {t('All conditions in a group must match.')}
                    </p>
                    {group.conditions.map((condition, conditionIndex) => (
                      <p key={conditionIndex} className='text-xs wrap-anywhere'>
                        {describeRequestCondition(condition, t)}
                      </p>
                    ))}
                  </div>
                  <Badge variant='outline'>{group.multiplier}×</Badge>
                </li>
              ))}
            </ul>
          ) : (
            <code className='bg-muted/40 block rounded-md p-2 text-xs wrap-anywhere whitespace-pre-wrap'>
              {split.requestRuleExpr}
            </code>
          )}
        </div>
      )}
      <details className='min-w-0' open={tiers.length === 0}>
        <summary className='text-muted-foreground focus-visible:ring-ring cursor-pointer rounded-sm text-xs focus-visible:ring-2 focus-visible:outline-none'>
          {t('Raw expression')}
        </summary>
        <code className='bg-muted/40 mt-2 block max-h-48 overflow-y-auto rounded-md p-2 text-xs wrap-anywhere whitespace-pre-wrap'>
          {props.billingExpr}
        </code>
      </details>
    </section>
  )
}
