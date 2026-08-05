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
*/
import {
  useEffect,
  useMemo,
  useState,
  type KeyboardEvent,
  type PointerEvent,
} from 'react'
import {
  ArrowDown,
  ArrowUp,
  ChevronsUpDown,
  CircleAlert,
  GripVertical,
  Route,
  Trash2,
} from 'lucide-react'
import { Reorder, useDragControls } from 'motion/react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { MAX_MANUAL_ROUTING_GROUPS } from '../lib/api-key-form'
import type { TokenRoutingMode } from '../types'
import {
  GroupRatioBadge,
  type ApiKeyGroupOption,
} from './api-key-group-combobox'

export type ApiKeyAutoRouteOption = {
  value: string
  label: string
  groups: string[]
}

type ApiKeyRoutingEditorProps = {
  mode: TokenRoutingMode
  route: string
  manualGroups: string[]
  retryOnFailure: boolean
  autoRouteOptions: ApiKeyAutoRouteOption[]
  realGroupOptions: ApiKeyGroupOption[]
  defaultManualGroups: string[]
  preserveUnavailableRouting?: boolean
  routesLoading?: boolean
  disabled?: boolean
  onModeChange: (mode: TokenRoutingMode) => void
  onRouteChange: (route: string) => void
  onManualGroupsChange: (groups: string[]) => void
  onRetryOnFailureChange: (enabled: boolean) => void
}

type ManualGroupRowProps = {
  group: string
  index: number
  count: number
  option?: ApiKeyGroupOption
  disabled?: boolean
  onMove: (index: number, direction: 'up' | 'down') => void
  onRemove: (group: string) => void
}

function ManualGroupRow(props: ManualGroupRowProps) {
  const { t } = useTranslation()
  const controls = useDragControls()
  const invalid = !props.option

  const startDrag = (event: PointerEvent<HTMLButtonElement>) => {
    if (!props.disabled) controls.start(event)
  }

  const handleDragKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      props.onMove(props.index, 'up')
    }
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      props.onMove(props.index, 'down')
    }
  }

  return (
    <Reorder.Item
      value={props.group}
      dragListener={false}
      dragControls={controls}
      className='bg-background flex min-h-11 items-center gap-1.5 border-b px-2 py-1.5 last:border-b-0 sm:gap-2'
    >
      <Button
        type='button'
        variant='ghost'
        size='icon-sm'
        disabled={props.disabled}
        className='text-muted-foreground cursor-grab touch-none active:cursor-grabbing'
        aria-label={t('Drag {{group}} to reorder', { group: props.group })}
        onPointerDown={startDrag}
        onKeyDown={handleDragKeyDown}
      >
        <GripVertical aria-hidden='true' />
      </Button>
      <span className='bg-muted text-muted-foreground flex size-5 shrink-0 items-center justify-center rounded-full text-[11px] font-medium tabular-nums'>
        {props.index + 1}
      </span>
      <span className='min-w-0 flex-1 truncate font-mono text-sm'>
        {props.option?.label || props.group}
      </span>
      {invalid ? (
        <Badge variant='destructive'>{t('Unavailable')}</Badge>
      ) : (
        <GroupRatioBadge ratio={props.option?.ratio} />
      )}
      <div className='flex shrink-0 items-center'>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.disabled || props.index === 0}
          aria-label={t('Move {{group}} up', { group: props.group })}
          onClick={() => props.onMove(props.index, 'up')}
        >
          <ArrowUp aria-hidden='true' />
        </Button>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.disabled || props.index === props.count - 1}
          aria-label={t('Move {{group}} down', { group: props.group })}
          onClick={() => props.onMove(props.index, 'down')}
        >
          <ArrowDown aria-hidden='true' />
        </Button>
        <Button
          type='button'
          variant='ghost'
          size='icon-sm'
          disabled={props.disabled}
          aria-label={t('Remove {{group}}', { group: props.group })}
          onClick={() => props.onRemove(props.group)}
        >
          <Trash2 aria-hidden='true' />
        </Button>
      </div>
    </Reorder.Item>
  )
}

type ManualGroupEditorProps = {
  value: string[]
  options: ApiKeyGroupOption[]
  disabled?: boolean
  onChange: (groups: string[]) => void
}

function ManualGroupEditor(props: ManualGroupEditorProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const optionMap = useMemo(
    () => new Map(props.options.map((option) => [option.value, option])),
    [props.options]
  )
  const atLimit = props.value.length >= MAX_MANUAL_ROUTING_GROUPS

  useEffect(() => {
    if (!props.disabled) return
    const timeoutId = window.setTimeout(() => setOpen(false), 0)
    return () => window.clearTimeout(timeoutId)
  }, [props.disabled])

  const toggleGroup = (group: string) => {
    if (props.disabled) return
    if (props.value.includes(group)) {
      props.onChange(props.value.filter((item) => item !== group))
      return
    }
    if (!atLimit) props.onChange([...props.value, group])
  }

  const moveGroup = (index: number, direction: 'up' | 'down') => {
    if (props.disabled) return
    const target = direction === 'up' ? index - 1 : index + 1
    if (target < 0 || target >= props.value.length) return
    const next = [...props.value]
    ;[next[index], next[target]] = [next[target], next[index]]
    props.onChange(next)
  }

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center justify-between gap-3'>
        <span className='text-sm font-medium'>{t('Manual group order')}</span>
        <span className='text-muted-foreground text-xs tabular-nums'>
          {t('{{count}} / {{max}} groups selected', {
            count: props.value.length,
            max: MAX_MANUAL_ROUTING_GROUPS,
          })}
        </span>
      </div>
      <Popover
        open={props.disabled ? false : open}
        onOpenChange={(nextOpen) => {
          if (!props.disabled) setOpen(nextOpen)
        }}
      >
        <PopoverTrigger
          render={
            <button
              type='button'
              role='combobox'
              disabled={props.disabled}
              aria-expanded={props.disabled ? false : open}
              aria-label={t('Select groups')}
              className={cn(
                'border-input bg-background focus-visible:border-ring focus-visible:ring-ring/20 flex min-h-10 w-full min-w-0 cursor-pointer flex-wrap items-center gap-1.5 rounded-md border px-2 py-1.5 text-left outline-none focus-visible:ring-[3px]',
                props.disabled && 'pointer-events-none opacity-50'
              )}
            />
          }
        >
          {props.value.length === 0 && (
            <span className='text-muted-foreground px-1 text-sm'>
              {t('Select groups')}
            </span>
          )}
          {props.value.map((group) => (
            <Badge key={group} variant='secondary' className='max-w-40'>
              <span className='truncate'>{group}</span>
            </Badge>
          ))}
          <ChevronsUpDown
            className='text-muted-foreground ml-auto size-4 shrink-0'
            aria-hidden='true'
          />
        </PopoverTrigger>
        <PopoverContent
          align='start'
          className='w-[var(--anchor-width)] min-w-72 p-0'
        >
          <Command>
            <CommandInput placeholder={t('Search groups')} />
            <CommandList>
              <CommandEmpty>{t('No groups found')}</CommandEmpty>
              <CommandGroup>
                {props.options.map((option) => {
                  const selected = props.value.includes(option.value)
                  return (
                    <CommandItem
                      key={option.value}
                      value={`${option.value} ${option.label} ${option.desc || ''}`}
                      data-checked={selected}
                      disabled={props.disabled || (!selected && atLimit)}
                      onSelect={() => toggleGroup(option.value)}
                    >
                      <span className='flex min-w-0 flex-1 flex-col'>
                        <span className='truncate font-medium'>
                          {option.label}
                        </span>
                        {option.desc && (
                          <span className='text-muted-foreground truncate text-xs'>
                            {option.desc}
                          </span>
                        )}
                      </span>
                      <GroupRatioBadge ratio={option.ratio} />
                    </CommandItem>
                  )
                })}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>

      <p className='text-muted-foreground text-xs'>
        {t('Requests try groups in the order shown below.')}
      </p>
      {props.value.length === 0 ? (
        <div className='border-border text-muted-foreground rounded-md border border-dashed px-3 py-6 text-center text-sm'>
          {t('Select at least one manual routing group')}
        </div>
      ) : (
        <Reorder.Group
          axis='y'
          values={props.value}
          onReorder={(groups) => {
            if (!props.disabled) props.onChange(groups)
          }}
          className='overflow-hidden rounded-md border'
        >
          {props.value.map((group, index) => (
            <ManualGroupRow
              key={group}
              group={group}
              index={index}
              count={props.value.length}
              option={optionMap.get(group)}
              disabled={props.disabled}
              onMove={moveGroup}
              onRemove={(removed) =>
                props.onChange(props.value.filter((item) => item !== removed))
              }
            />
          ))}
        </Reorder.Group>
      )}
    </div>
  )
}

type AutomaticRouteSelectionProps = Pick<
  ApiKeyRoutingEditorProps,
  'autoRouteOptions' | 'disabled' | 'onRouteChange' | 'route' | 'routesLoading'
>

function AutomaticRouteSelection(props: AutomaticRouteSelectionProps) {
  const { t } = useTranslation()

  if (props.routesLoading) {
    return (
      <div className='grid gap-2 sm:grid-cols-2'>
        <Skeleton className='h-20 w-full rounded-md' />
        <Skeleton className='h-20 w-full rounded-md' />
      </div>
    )
  }

  if (props.autoRouteOptions.length === 0) {
    return (
      <Alert variant='destructive'>
        <CircleAlert aria-hidden='true' />
        <AlertTitle>{t('No automatic routing groups')}</AlertTitle>
        <AlertDescription>
          {t(
            'No automatic routing groups are currently available. Select manual routing or contact an administrator.'
          )}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <RadioGroup
      value={props.route}
      onValueChange={props.onRouteChange}
      className='grid gap-2 sm:grid-cols-2'
    >
      {props.autoRouteOptions.map((option) => {
        const selected = props.route === option.value
        return (
          <label
            key={option.value}
            className={cn(
              'border-border bg-background hover:bg-muted/25 flex min-h-20 cursor-pointer items-start gap-3 rounded-md border px-3 py-2.5 transition-colors',
              selected && 'border-foreground bg-muted/40',
              props.disabled && 'pointer-events-none opacity-50'
            )}
          >
            <Route
              className='text-muted-foreground mt-0.5 size-4 shrink-0'
              aria-hidden='true'
            />
            <span className='min-w-0 flex-1'>
              <span className='block truncate text-sm font-medium'>
                {option.label}
              </span>
              <span className='text-muted-foreground block truncate font-mono text-[11px]'>
                {option.value}
              </span>
              <span className='mt-1 flex flex-wrap gap-1'>
                {option.groups.slice(0, 3).map((group) => (
                  <Badge
                    key={group}
                    variant='secondary'
                    className='max-w-24 truncate font-mono text-[10px]'
                  >
                    {group}
                  </Badge>
                ))}
                {option.groups.length > 3 && (
                  <Badge variant='outline' className='text-[10px]'>
                    +{option.groups.length - 3}
                  </Badge>
                )}
              </span>
            </span>
            <RadioGroupItem
              value={option.value}
              aria-label={option.label}
              disabled={props.disabled}
            />
          </label>
        )
      })}
    </RadioGroup>
  )
}

export function ApiKeyRoutingEditor(props: ApiKeyRoutingEditorProps) {
  const { t } = useTranslation()
  const smart = props.mode === 'smart'
  const activeAutoRoute = props.autoRouteOptions.find(
    (option) => option.value === props.route
  )

  const setMode = (mode: TokenRoutingMode) => {
    if (mode === 'manual' && props.manualGroups.length === 0) {
      props.onManualGroupsChange(
        props.defaultManualGroups.slice(0, MAX_MANUAL_ROUTING_GROUPS)
      )
    }
    props.onModeChange(mode)
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='border-border bg-muted/25 flex flex-col items-stretch gap-2 rounded-md border px-3 py-2.5 sm:flex-row sm:items-center sm:gap-3'>
        <Route
          className='text-muted-foreground size-4 shrink-0'
          aria-hidden='true'
        />
        <p className='text-muted-foreground min-w-0 flex-1 text-xs sm:text-sm'>
          {smart
            ? t(
                'Automatic routing is enabled. The system follows the selected automatic route group order.'
              )
            : t(
                'Automatic routing is disabled. The manual group order is used.'
              )}
        </p>
        <Button
          type='button'
          size='sm'
          variant={smart ? 'default' : 'secondary'}
          disabled={props.disabled}
          className='w-full shrink-0 sm:w-auto'
          onClick={() => setMode(smart ? 'manual' : 'smart')}
        >
          {smart
            ? t('Disable and select groups')
            : t('Enable automatic routing')}
        </Button>
      </div>

      {smart ? (
        <div className='flex flex-col gap-4'>
          <div className='flex flex-col gap-2'>
            <div>
              <p className='text-sm font-medium'>
                {t('Automatic routing group')}
              </p>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Choose a system-defined group chain. Requests follow its configured order.'
                )}
              </p>
            </div>
            <AutomaticRouteSelection
              autoRouteOptions={props.autoRouteOptions}
              disabled={props.disabled}
              onRouteChange={props.onRouteChange}
              route={props.route}
              routesLoading={props.routesLoading}
            />
            {props.route && !props.routesLoading && !activeAutoRoute && (
              <Alert variant='destructive'>
                <CircleAlert aria-hidden='true' />
                <AlertTitle>{t('Routing group unavailable')}</AlertTitle>
                <AlertDescription>
                  {props.preserveUnavailableRouting
                    ? t(
                        'The saved routing group {{route}} is not available for new selections. It will be preserved unless routing is changed.',
                        { route: props.route }
                      )
                    : t(
                        'The saved routing group {{route}} is no longer available. Select another group before saving.',
                        { route: props.route }
                      )}
                </AlertDescription>
              </Alert>
            )}
          </div>
        </div>
      ) : (
        <ManualGroupEditor
          value={props.manualGroups}
          options={props.realGroupOptions}
          disabled={props.disabled}
          onChange={props.onManualGroupsChange}
        />
      )}

      <div
        className={cn(
          'border-border flex items-center justify-between gap-4',
          smart ? 'border-t pt-3' : 'border-y py-3'
        )}
      >
        <div className='min-w-0'>
          <p className='text-sm font-medium'>{t('Cross-group retry')}</p>
          <p className='text-muted-foreground text-xs'>
            {smart
              ? t(
                  'If the current group fails, try the next group in the configured automatic route order.'
                )
              : t(
                  'If the current group fails, try the next group in the manual order.'
                )}
          </p>
        </div>
        <Switch
          checked={props.retryOnFailure}
          disabled={props.disabled}
          onCheckedChange={props.onRetryOnFailureChange}
        />
      </div>
    </div>
  )
}
