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
import { useState } from 'react'
import { SlidersHorizontalIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useIsMobile } from '@/hooks/use-mobile'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { PromptInputButton } from '@/components/ai-elements/prompt-input'
import {
  getParameterControlValueText,
  normalizeParameterNumberValue,
  PLAYGROUND_PARAMETER_CONTROLS,
  PLAYGROUND_PARAMETER_PANEL_SCROLL_CLASS,
  type PlaygroundParameterKey,
} from '../lib/playground-parameters'
import type { ParameterEnabled, PlaygroundConfig } from '../types'

type PlaygroundParameterPanelProps = {
  config: PlaygroundConfig
  disabled?: boolean
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onParameterEnabledChange: (
    key: PlaygroundParameterKey,
    value: boolean
  ) => void
  parameterEnabled: ParameterEnabled
}

type PlaygroundParameterContentProps = PlaygroundParameterPanelProps & {
  compact?: boolean
}

function PlaygroundParameterContent(props: PlaygroundParameterContentProps) {
  const { t } = useTranslation()
  const [parameterDrafts, setParameterDrafts] = useState<
    Partial<Record<PlaygroundParameterKey, string>>
  >({})

  const updateParameterConfig = (
    key: PlaygroundParameterKey,
    value: number | null
  ) => {
    if (key === 'seed') {
      props.onConfigChange('seed', value)
      return
    }

    props.onConfigChange(key, value ?? 0)
  }

  const commitParameterDraft = (
    key: PlaygroundParameterKey,
    rawValue: string
  ) => {
    updateParameterConfig(key, normalizeParameterNumberValue(key, rawValue))
    setParameterDrafts((current) => {
      const next = { ...current }
      delete next[key]
      return next
    })
  }

  return (
    <div
      className={cn(
        'grid gap-3',
        PLAYGROUND_PARAMETER_PANEL_SCROLL_CLASS,
        props.compact ? 'px-4 pb-4' : 'p-1'
      )}
    >
      {PLAYGROUND_PARAMETER_CONTROLS.map((control) => {
        const enabled = props.parameterEnabled[control.key]
        const value = props.config[control.key]
        const controlId = `playground-${control.key}`

        return (
          <div
            className={cn(
              'border-border/70 bg-background/60 grid gap-2 rounded-lg border p-3 transition-opacity',
              (!enabled || props.disabled) && 'opacity-55'
            )}
            key={control.key}
          >
            <div className='flex items-start justify-between gap-3'>
              <div className='min-w-0 space-y-1'>
                <div className='flex min-w-0 items-center gap-2'>
                  <label
                    className='truncate text-sm leading-5 font-medium'
                    htmlFor={controlId}
                  >
                    {t(control.labelKey)}
                  </label>
                  <Badge
                    className='h-5 max-w-24 shrink-0 px-1.5 font-mono text-[11px]'
                    variant='outline'
                  >
                    {t(getParameterControlValueText(control.key, value))}
                  </Badge>
                </div>
                <p className='text-muted-foreground text-xs leading-4'>
                  {t(control.descriptionKey)}
                </p>
              </div>

              <Switch
                aria-label={t('Enable {{parameter}}', {
                  parameter: t(control.labelKey),
                })}
                checked={enabled}
                disabled={props.disabled}
                onCheckedChange={(checked) =>
                  props.onParameterEnabledChange(control.key, checked)
                }
                size='sm'
              />
            </div>

            {control.valueType === 'slider' ? (
              <Slider
                className='py-1.5'
                disabled={props.disabled || !enabled}
                id={controlId}
                max={control.max}
                min={control.min}
                onValueChange={(nextValue) => {
                  const firstValue = Array.isArray(nextValue)
                    ? nextValue[0]
                    : nextValue
                  updateParameterConfig(
                    control.key,
                    normalizeParameterNumberValue(control.key, firstValue)
                  )
                }}
                step={control.step}
                value={[Number(value)]}
              />
            ) : (
              <Input
                disabled={props.disabled || !enabled}
                id={controlId}
                inputMode='numeric'
                max={control.max}
                min={control.min}
                onChange={(event) =>
                  setParameterDrafts((current) => ({
                    ...current,
                    [control.key]: event.target.value,
                  }))
                }
                onBlur={(event) =>
                  commitParameterDraft(control.key, event.currentTarget.value)
                }
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    event.preventDefault()
                    event.currentTarget.blur()
                  }
                }}
                step={control.step}
                type='number'
                value={parameterDrafts[control.key] ?? value ?? ''}
              />
            )}
          </div>
        )
      })}
    </div>
  )
}

export function PlaygroundParameterPanel(props: PlaygroundParameterPanelProps) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const activeCount = PLAYGROUND_PARAMETER_CONTROLS.filter(
    (control) => props.parameterEnabled[control.key]
  ).length

  const trigger = (
    <PromptInputButton
      aria-label={t('Parameters')}
      className='text-muted-foreground hover:bg-muted/70 hover:text-foreground relative'
      disabled={props.disabled}
      variant='ghost'
    >
      <SlidersHorizontalIcon size={16} />
      <span className='bg-primary text-primary-foreground absolute -top-1 -right-1 flex h-3.5 min-w-3.5 items-center justify-center rounded-full px-1 text-[9px] leading-none font-semibold'>
        {activeCount}
      </span>
    </PromptInputButton>
  )

  if (isMobile) {
    return (
      <Sheet>
        <Tooltip>
          <TooltipTrigger render={<SheetTrigger render={trigger} />} />
          <TooltipContent>
            <p>{t('Parameters')}</p>
          </TooltipContent>
        </Tooltip>
        <SheetContent
          className='max-h-[85vh] overflow-hidden rounded-t-xl'
          side='bottom'
        >
          <SheetHeader>
            <SheetTitle>{t('Parameter settings')}</SheetTitle>
          </SheetHeader>
          <PlaygroundParameterContent {...props} compact />
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger render={<PopoverTrigger render={trigger} />} />
        <TooltipContent>
          <p>{t('Parameters')}</p>
        </TooltipContent>
      </Tooltip>
      <PopoverContent
        align='start'
        className='w-[22rem] max-w-[calc(100vw-2rem)] gap-3 p-3'
        collisionPadding={8}
        side='top'
        sideOffset={8}
      >
        <div className='space-y-1 px-1'>
          <div className='text-sm font-semibold'>{t('Parameter settings')}</div>
          <div className='text-muted-foreground text-xs leading-4'>
            {t('Only enabled parameters are sent with the request.')}
          </div>
        </div>
        <PlaygroundParameterContent {...props} />
      </PopoverContent>
    </Popover>
  )
}
