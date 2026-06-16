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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Braces, Check, WandSparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { updateTieredBillingConfig } from '../api'
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageActionsPortal } from '../components/settings-page-context'
import { formatJsonForTextarea } from './utils'

type TieredBillingEntry = {
  enabled: boolean
  expr: string
}

type TieredBillingSettingsProps = {
  billingMode: string
  billingExpr: string
}

const EXAMPLE_CONFIG: Record<string, TieredBillingEntry> = {
  'claude-sonnet-4': {
    enabled: true,
    expr: 'len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3) : tier("long_context", p * 6 + c * 22.5 + cr * 0.6)',
  },
  'gpt-image-2': {
    enabled: true,
    expr: 'tier("base", p * 5 + c * 20 + img * 10)',
  },
}

function parseRecord<T>(raw: string, fallback: Record<string, T>) {
  if (!raw || raw.trim() === '') return fallback
  const parsed = JSON.parse(raw) as unknown
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return fallback
  }
  return parsed as Record<string, T>
}

function buildUnifiedConfig(billingMode: string, billingExpr: string) {
  const modeMap = parseRecord<string>(billingMode, {})
  const exprMap = parseRecord<string>(billingExpr, {})
  const modelNames = new Set([...Object.keys(modeMap), ...Object.keys(exprMap)])
  const config: Record<string, TieredBillingEntry> = {}

  Array.from(modelNames)
    .sort((a, b) => a.localeCompare(b))
    .forEach((model) => {
      const enabled = modeMap[model] === 'tiered_expr'
      const expr = exprMap[model] || ''
      if (enabled || expr) {
        config[model] = { enabled, expr }
      }
    })

  return config
}

function validateUnifiedConfig(value: string) {
  const parsed = JSON.parse(value) as unknown
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    throw new Error('Tiered billing JSON must be an object')
  }

  const normalized: Record<string, TieredBillingEntry> = {}
  Object.entries(parsed as Record<string, unknown>).forEach(
    ([model, entry]) => {
      const name = model.trim()
      if (!name) {
        throw new Error('Model name cannot be empty')
      }
      if (!entry || typeof entry !== 'object' || Array.isArray(entry)) {
        throw new Error(`Config for ${name} must be an object`)
      }

      const rawEntry = entry as Partial<TieredBillingEntry>
      const enabled = Boolean(rawEntry.enabled)
      const expr = typeof rawEntry.expr === 'string' ? rawEntry.expr.trim() : ''
      if (enabled && !expr) {
        throw new Error(`Billing expression for ${name} cannot be empty`)
      }
      normalized[name] = { enabled, expr }
    }
  )

  return normalized
}

export function TieredBillingSettings(props: TieredBillingSettingsProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [text, setText] = useState('')
  const [error, setError] = useState('')

  const initialText = useMemo(() => {
    return JSON.stringify(
      buildUnifiedConfig(props.billingMode, props.billingExpr),
      null,
      2
    )
  }, [props.billingExpr, props.billingMode])

  useEffect(() => {
    setText(initialText)
    setError('')
  }, [initialText])

  const mutation = useMutation({
    mutationFn: updateTieredBillingConfig,
    onSuccess: (data) => {
      if (data.success) {
        toast.success(t('Tiered billing configuration saved'))
        queryClient.invalidateQueries({ queryKey: ['system-options'] })
      } else {
        toast.error(data.message || t('Failed to save tiered billing config'))
      }
    },
    onError: (err: Error) => {
      toast.error(err.message || t('Failed to save tiered billing config'))
    },
  })

  const handleFormat = useCallback(() => {
    try {
      setText(formatJsonForTextarea(text || '{}'))
      setError('')
    } catch (err) {
      setError(err instanceof Error ? err.message : t('Invalid JSON'))
    }
  }, [t, text])

  const handleLoadExample = useCallback(() => {
    setText(JSON.stringify(EXAMPLE_CONFIG, null, 2))
    setError('')
  }, [])

  const handleSave = useCallback(async () => {
    let config: Record<string, TieredBillingEntry>
    try {
      config = validateUnifiedConfig(text || '{}')
      setError('')
    } catch (err) {
      const message = err instanceof Error ? err.message : t('Invalid JSON')
      setError(message)
      toast.error(message)
      return
    }

    await mutation.mutateAsync({ config })
  }, [mutation, t, text])

  const enabledCount = useMemo(() => {
    try {
      const config = validateUnifiedConfig(text || '{}')
      return Object.values(config).filter((entry) => entry.enabled).length
    } catch {
      return 0
    }
  }, [text])

  return (
    <SettingsForm>
      <SettingsPageActionsPortal>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={handleFormat}
        >
          <Braces className='mr-2 h-4 w-4' />
          {t('Format JSON')}
        </Button>
        <Button
          type='button'
          size='sm'
          onClick={handleSave}
          disabled={mutation.isPending || Boolean(error)}
        >
          {mutation.isPending ? t('Saving...') : t('Save tiered billing')}
        </Button>
      </SettingsPageActionsPortal>

      <div className='space-y-4'>
        <div className='flex flex-wrap items-start justify-between gap-3'>
          <div className='space-y-1'>
            <h3 className='text-base font-medium'>
              {t('Tiered billing JSON')}
            </h3>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Manage tiered billing rules in one JSON document. Saving updates billing mode and billing expression atomically.'
              )}
            </p>
          </div>
          <Button type='button' variant='outline' onClick={handleLoadExample}>
            <WandSparkles className='mr-2 h-4 w-4' />
            {t('Load example')}
          </Button>
        </div>

        <Alert>
          <Check className='h-4 w-4' />
          <AlertDescription>
            {t('{{count}} tiered billing models enabled', {
              count: enabledCount,
            })}
          </AlertDescription>
        </Alert>

        {error && (
          <Alert variant='destructive'>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <Textarea
          value={text}
          onChange={(event) => {
            setText(event.target.value)
            if (error) setError('')
          }}
          rows={22}
          spellCheck={false}
          className='font-mono text-xs leading-5'
        />

        <div className='text-muted-foreground space-y-2 text-sm'>
          <p>
            {t(
              'Schema: model name maps to { enabled, expr }. Disabled entries remove the model from tiered billing while keeping the JSON visible before save.'
            )}
          </p>
          <p>
            {t(
              'Expression coefficients are USD prices per 1M tokens. Use len for context-length tiers and tier(name, value) to label the matched tier.'
            )}
          </p>
        </div>
      </div>
    </SettingsForm>
  )
}
