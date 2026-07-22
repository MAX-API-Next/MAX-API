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
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Slider } from '@/components/ui/slider'
import { Switch } from '@/components/ui/switch'
import {
  getParameterControlValueText,
  normalizeParameterNumberValue,
  type PlaygroundParameterControl,
} from '../lib/playground-parameters'

type PlaygroundParameterRowProps = {
  control: PlaygroundParameterControl
  disabled?: boolean
  enabled: boolean
  onEnabledChange: (value: boolean) => void
  onValueChange: (value: number | null) => void
  value: number | null
}

export function PlaygroundParameterRow(props: PlaygroundParameterRowProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<string>()
  const controlId = `playground-${props.control.key}`

  const updateValue = (value: string | number) => {
    props.onValueChange(normalizeParameterNumberValue(props.control.key, value))
  }

  const commitDraft = (rawValue: string) => {
    updateValue(rawValue)
    setDraft(undefined)
  }

  return (
    <div
      className={cn(
        'border-border/70 bg-background/60 grid gap-2 rounded-lg border p-3 transition-opacity',
        (!props.enabled || props.disabled) && 'opacity-55'
      )}
    >
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0 space-y-1'>
          <div className='flex min-w-0 items-center gap-2'>
            <label
              className='truncate text-sm leading-5 font-medium'
              htmlFor={controlId}
            >
              {t(props.control.labelKey)}
            </label>
            <Badge
              className='h-5 max-w-24 shrink-0 px-1.5 font-mono text-[11px]'
              variant='outline'
            >
              {t(getParameterControlValueText(props.control.key, props.value))}
            </Badge>
          </div>
          <p className='text-muted-foreground text-xs leading-4'>
            {t(props.control.descriptionKey)}
          </p>
        </div>

        <Switch
          aria-label={t('Enable {{parameter}}', {
            parameter: t(props.control.labelKey),
          })}
          checked={props.enabled}
          disabled={props.disabled}
          onCheckedChange={props.onEnabledChange}
          size='sm'
        />
      </div>

      {props.control.valueType === 'slider' ? (
        <Slider
          className='py-1.5'
          disabled={props.disabled || !props.enabled}
          id={controlId}
          max={props.control.max}
          min={props.control.min}
          onValueChange={(nextValue) => {
            const firstValue = Array.isArray(nextValue)
              ? nextValue[0]
              : nextValue
            updateValue(firstValue)
          }}
          step={props.control.step}
          value={[Number(props.value)]}
        />
      ) : (
        <Input
          disabled={props.disabled || !props.enabled}
          id={controlId}
          inputMode='numeric'
          max={props.control.max}
          min={props.control.min}
          onBlur={(event) => commitDraft(event.currentTarget.value)}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter') {
              event.preventDefault()
              event.currentTarget.blur()
            }
          }}
          step={props.control.step}
          type='number'
          value={draft ?? props.value ?? ''}
        />
      )}
    </div>
  )
}
