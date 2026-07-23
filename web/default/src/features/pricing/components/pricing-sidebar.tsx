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
import type { ReactNode } from 'react'
import { ChevronDown, Info, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { AutoGroupRoute } from '@/lib/auto-routes'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { GroupBadge } from '@/components/group-badge'
import {
  ENDPOINT_TYPES,
  FILTER_ALL,
  QUOTA_TYPES,
  getEndpointTypeLabels,
  getQuotaTypeLabels,
} from '../constants'
import {
  getAutoRouteLabelOverride,
  getConfiguredAutoRouteChains,
} from '../lib/auto-route-view'
import { parseTags } from '../lib/filters'
import type { PricingModel, PricingVendor } from '../types'

type FilterOption = {
  value: string
  label: string
  count?: number
  suffix?: string
  icon?: ReactNode
}

type FilterSectionProps = {
  title: string
  value: string
  options: FilterOption[]
  onChange: (value: string) => void
  children?: ReactNode
}

export interface PricingSidebarProps {
  quotaTypeFilter: string
  endpointTypeFilter: string
  vendorFilter: string
  groupFilter: string
  tagFilter: string
  onQuotaTypeChange: (value: string) => void
  onEndpointTypeChange: (value: string) => void
  onVendorChange: (value: string) => void
  onGroupChange: (value: string) => void
  onTagChange: (value: string) => void
  vendors: PricingVendor[]
  groups: string[]
  groupRatios?: Record<string, number>
  autoGroups?: string[]
  autoRoutes?: AutoGroupRoute[]
  tags: string[]
  models: PricingModel[]
  hasActiveFilters: boolean
  onClearFilters: () => void
  className?: string
}

function countBy(
  models: PricingModel[],
  predicate: (model: PricingModel) => boolean
): number {
  return models.reduce((count, model) => count + (predicate(model) ? 1 : 0), 0)
}

function formatGroupRatio(ratio: number | undefined): string | undefined {
  if (ratio == null) return undefined
  const formatted = Number.isInteger(ratio)
    ? ratio.toString()
    : ratio.toFixed(3).replace(/0+$/, '').replace(/\.$/, '')
  return `x${formatted}`
}

function FilterChip(props: {
  option: FilterOption
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type='button'
      onClick={props.onClick}
      className={cn(
        'group inline-flex max-w-full items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium transition-all',
        props.active
          ? 'border-foreground/30 bg-foreground/5 text-foreground shadow-sm'
          : 'border-border/70 bg-background text-muted-foreground hover:border-border hover:bg-muted/50 hover:text-foreground'
      )}
      title={props.option.label}
    >
      {props.option.icon && (
        <span className='shrink-0'>{props.option.icon}</span>
      )}
      <span className='truncate'>{props.option.label}</span>
      {(props.option.suffix || props.option.count != null) && (
        <span
          className={cn(
            'rounded-md px-1.5 py-0.5 text-[10px]',
            props.active
              ? 'bg-background text-foreground'
              : 'bg-muted text-muted-foreground'
          )}
        >
          {props.option.suffix ?? props.option.count}
        </span>
      )}
    </button>
  )
}

function FilterSection(props: FilterSectionProps) {
  return (
    <Collapsible
      defaultOpen
      className='border-border/70 border-b pb-3 last:border-b-0'
    >
      <CollapsibleTrigger className='group flex w-full items-center justify-between py-2.5 text-left'>
        <span className='text-foreground text-sm font-semibold'>
          {props.title}
        </span>
        <ChevronDown className='text-muted-foreground size-4 transition-transform group-data-[panel-open]:rotate-180' />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className='flex flex-wrap gap-1.5'>
          {props.options.map((option) => (
            <FilterChip
              key={option.value}
              option={option}
              active={props.value === option.value}
              onClick={() => props.onChange(option.value)}
            />
          ))}
        </div>
        {props.children}
      </CollapsibleContent>
    </Collapsible>
  )
}

function AutoRouteGroupIntro(props: {
  autoGroups?: string[]
  autoRoutes?: AutoGroupRoute[]
  visibleGroups: string[]
}) {
  const { t } = useTranslation()
  const visibleGroupSet = new Set(props.visibleGroups)
  const routeChains = getConfiguredAutoRouteChains({
    autoGroups: props.autoGroups ?? [],
    autoRoutes: props.autoRoutes,
    groupFilter: (group) => visibleGroupSet.has(group),
  })

  if (routeChains.length === 0) return null

  return (
    <Alert className='mt-3'>
      <Info aria-hidden />
      <AlertTitle className='text-xs'>
        {t('Configured auto route groups')}
      </AlertTitle>
      <AlertDescription className='flex flex-col gap-2 text-xs leading-relaxed'>
        <p>
          {t(
            'Auto route groups are selectable aliases that point to administrator-configured billing groups.'
          )}
        </p>
        <Collapsible>
          <CollapsibleTrigger
            render={
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='group h-auto w-full justify-between gap-2 px-2.5 py-2 text-left'
              />
            }
          >
            <span className='flex min-w-0 items-center gap-2'>
              <span className='text-foreground truncate font-medium'>
                {t('Auto route chains')}
              </span>
              <Badge
                variant='secondary'
                className='h-5 px-1.5 text-[10px] tabular-nums'
              >
                {routeChains.length}
              </Badge>
            </span>
            <ChevronDown
              data-icon='inline-end'
              className='text-muted-foreground shrink-0 transition-transform group-data-[panel-open]:rotate-180'
              aria-hidden='true'
            />
          </CollapsibleTrigger>
          <CollapsibleContent className='mt-2'>
            <div className='bg-background/70 flex max-h-52 flex-col gap-1.5 overflow-y-auto overscroll-contain rounded-md border px-2.5 py-2'>
              {routeChains.map(({ route, groups }) => (
                <div
                  key={route.key}
                  className='flex flex-wrap items-center gap-1'
                >
                  <GroupBadge
                    group={route.key}
                    label={getAutoRouteLabelOverride(route)}
                    size='sm'
                  />
                  <span className='text-muted-foreground/40'>→</span>
                  {groups.map((group, index) => (
                    <span
                      key={`${route.key}-${group}-${index}`}
                      className='flex items-center gap-1'
                    >
                      <GroupBadge group={group} size='sm' />
                      {index < groups.length - 1 && (
                        <span className='text-muted-foreground/40'>→</span>
                      )}
                    </span>
                  ))}
                </div>
              ))}
            </div>
          </CollapsibleContent>
        </Collapsible>
        <p>
          {t(
            'Billing and group ratios are calculated from the real group that handles the request.'
          )}
        </p>
      </AlertDescription>
    </Alert>
  )
}

export function PricingSidebar(props: PricingSidebarProps) {
  const { t } = useTranslation()
  const quotaTypeLabels = getQuotaTypeLabels(t)
  const endpointTypeLabels = getEndpointTypeLabels(t)

  const vendorOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: t('All Vendors'),
      count: props.models.length,
    },
    ...props.vendors
      .map((vendor) => ({
        value: vendor.name,
        label: vendor.name,
        count: countBy(
          props.models,
          (model) => model.vendor_name === vendor.name
        ),
        icon: vendor.icon ? getLobeIcon(vendor.icon, 14) : undefined,
      }))
      .filter((vendor) => vendor.count > 0),
  ]

  const groupOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: t('All Groups'),
    },
    ...props.groups.map((group) => ({
      value: group,
      label: group,
      suffix: formatGroupRatio(props.groupRatios?.[group]),
    })),
  ]

  const quotaOptions: FilterOption[] = [
    {
      value: QUOTA_TYPES.ALL,
      label: quotaTypeLabels[QUOTA_TYPES.ALL],
      count: props.models.length,
    },
    {
      value: QUOTA_TYPES.TOKEN,
      label: quotaTypeLabels[QUOTA_TYPES.TOKEN],
      count: countBy(props.models, (model) => model.quota_type === 0),
    },
    {
      value: QUOTA_TYPES.REQUEST,
      label: quotaTypeLabels[QUOTA_TYPES.REQUEST],
      count: countBy(props.models, (model) => model.quota_type === 1),
    },
  ]

  const tagOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: t('All Tags'),
      count: props.models.length,
    },
    ...props.tags.map((tag) => ({
      value: tag,
      label: tag,
      count: countBy(props.models, (model) =>
        parseTags(model.tags)
          .map((item) => item.toLowerCase())
          .includes(tag.toLowerCase())
      ),
    })),
  ]

  const endpointOptions: FilterOption[] = [
    {
      value: ENDPOINT_TYPES.ALL,
      label: endpointTypeLabels[ENDPOINT_TYPES.ALL],
      count: props.models.length,
    },
    ...Object.entries(endpointTypeLabels)
      .filter(([value]) => value !== ENDPOINT_TYPES.ALL)
      .map(([value, label]) => ({
        value,
        label,
        count: countBy(
          props.models,
          (model) => model.supported_endpoint_types?.includes(value) ?? false
        ),
      })),
  ]

  return (
    <aside className={cn('rounded-xl border p-3', props.className)}>
      <div className='mb-2.5 flex items-center justify-between gap-2'>
        <div>
          <h2 className='text-foreground text-sm font-bold'>{t('Filter')}</h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('Refine models by provider, group, type, and tags.')}
          </p>
        </div>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          onClick={props.onClearFilters}
          disabled={!props.hasActiveFilters}
          className='h-7 gap-1.5 px-2 text-xs'
        >
          <RotateCcw className='size-3.5' />
          {t('Reset')}
        </Button>
      </div>

      {props.hasActiveFilters && (
        <Badge variant='secondary' className='mb-3'>
          {t('Filters active')}
        </Badge>
      )}

      <div className='space-y-1'>
        <FilterSection
          title={t('Groups')}
          value={props.groupFilter}
          options={groupOptions}
          onChange={props.onGroupChange}
        >
          <AutoRouteGroupIntro
            autoGroups={props.autoGroups}
            autoRoutes={props.autoRoutes}
            visibleGroups={props.groups}
          />
        </FilterSection>
        <FilterSection
          title={t('All Vendors')}
          value={props.vendorFilter}
          options={vendorOptions}
          onChange={props.onVendorChange}
        />
        <FilterSection
          title={t('Model Tags')}
          value={props.tagFilter}
          options={tagOptions}
          onChange={props.onTagChange}
        />
        <FilterSection
          title={t('Pricing Type')}
          value={props.quotaTypeFilter}
          options={quotaOptions}
          onChange={props.onQuotaTypeChange}
        />
        <FilterSection
          title={t('Endpoint Type')}
          value={props.endpointTypeFilter}
          options={endpointOptions}
          onChange={props.onEndpointTypeChange}
        />
      </div>
    </aside>
  )
}
