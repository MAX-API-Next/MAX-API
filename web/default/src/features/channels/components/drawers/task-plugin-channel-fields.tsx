// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
// Interaction reference: QuantumNous/new-api 8542c9d6, channel plugin config
// fields and plugin option picker (AGPL-3.0-or-later).
import { useEffect } from 'react'
import { useFormContext, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import type { PluginEntry } from '@/features/task-plugins/api'
import type { ChannelFormValues } from '../../lib'
import { readTaskPluginSettings } from '../../lib/task-plugin-channel'

const selectClass = 'bg-background w-full rounded-md border p-2'

export function TaskPluginChannelFields(props: {
  options: PluginEntry[]
  loading: boolean
  failed: boolean
  refresh: () => void
}) {
  const { t } = useTranslation()
  const { control, setValue, getValues, register } =
    useFormContext<ChannelFormValues>()
  const setting = useWatch({ control, name: 'setting' })
  const observed = useWatch({ control, name: 'expected_task_plugin_digest' })
  const settings = readTaskPluginSettings(setting)
  const key = settings.task_plugin_key
  const selected = props.options.find((option) => option.key === key)
  const schema = selected?.channel_config
  const config = settings.task_plugin_config
  const dirty = { shouldDirty: true, shouldValidate: true }

  useEffect(() => {
    if (selected && !getValues('expected_task_plugin_digest')) {
      setValue('expected_task_plugin_digest', selected.digest)
    }
  }, [selected, getValues, setValue])

  function changePlugin(nextKey: string) {
    const next = props.options.find((option) => option.key === nextKey)
    setValue(
      'setting',
      JSON.stringify({
        task_plugin_key: nextKey,
        task_plugin_upstream: next?.upstreams?.[0] ?? 'vendor',
        task_plugin_config: {},
      }),
      dirty
    )
    setValue('expected_task_plugin_digest', next?.digest)
    setValue('base_url', next?.base_url ?? '', dirty)
    setValue('other', '', dirty)
    setValue('key', '', dirty)
    setValue('models', '', dirty)
    setValue('model_mapping', '', dirty)
  }

  function changeParameter(name: string, value?: string | number | boolean) {
    const next =
      value === undefined ? { ...config } : { ...config, [name]: value }
    if (value === undefined) delete next[name]
    setValue(
      'setting',
      JSON.stringify({ ...settings, task_plugin_config: next }),
      dirty
    )
  }

  return (
    <div className='space-y-3'>
      <p className='text-muted-foreground text-sm'>
        {t(
          'The channel follows the active plugin version. Production task admission is unavailable.'
        )}
      </p>
      <label className='grid gap-1 text-sm'>
        {t('Task Plugin')}
        <select
          className={selectClass}
          value={key}
          onChange={(e) => changePlugin(e.target.value)}
        >
          <option value=''>{t('Select a task plugin')}</option>
          {key && !selected && (
            <option value={key}>
              {key} ({t('Unavailable')})
            </option>
          )}
          {props.options.map((option) => (
            <option key={option.key} value={option.key}>
              {option.name} · {option.version}
            </option>
          ))}
        </select>
      </label>
      {props.loading && (
        <p role='status'>{t('Loading task plugin options…')}</p>
      )}
      {props.failed && (
        <p role='alert'>{t('Task plugin catalog could not be loaded')}</p>
      )}
      {!props.loading && !props.failed && !props.options.length && (
        <p className='text-muted-foreground text-sm'>
          {t('Activate a compatible task plugin in the plugin catalog first.')}
        </p>
      )}
      {key && !selected && !props.loading && (
        <p role='alert' className='text-destructive text-sm'>
          {t(
            'The saved task plugin is unavailable. Select another active plugin or refresh the options.'
          )}
        </p>
      )}
      <Button
        type='button'
        size='sm'
        variant='outline'
        disabled={props.loading}
        onClick={props.refresh}
      >
        {t('Refresh plugin options')}
      </Button>
      {selected && observed && observed !== selected.digest && (
        <div className='space-y-2'>
          <p role='alert' className='text-sm'>
            {t(
              'The active plugin version changed. Review the models and parameters before saving.'
            )}
          </p>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={() =>
              setValue('expected_task_plugin_digest', selected.digest)
            }
          >
            {t('Use active version')}
          </Button>
        </div>
      )}
      {selected && (
        <>
          <label className='grid gap-1 text-sm'>
            {t('Upstream kind')}
            <select
              className={selectClass}
              value={settings.task_plugin_upstream}
              onChange={(e) =>
                setValue(
                  'setting',
                  JSON.stringify({
                    ...settings,
                    task_plugin_upstream: e.target.value,
                  }),
                  dirty
                )
              }
            >
              {(selected.upstreams ?? ['vendor', 'new_api']).map((kind) => (
                <option key={kind} value={kind}>
                  {kind === 'vendor' ? t('Vendor') : 'New API'}
                </option>
              ))}
            </select>
          </label>
          {selected.key_hint && (
            <p className='text-muted-foreground text-sm'>{selected.key_hint}</p>
          )}
          {selected.region_parameter && (
            <label className='grid gap-1 text-sm'>
              {t('Region or model-to-region JSON')}
              <Input {...register('other')} placeholder='global' />
            </label>
          )}
          {Object.entries(schema?.properties ?? {}).map(([name, field]) => {
            const value = Object.hasOwn(config, name) ? config[name] : undefined
            const required =
              schema?.required?.includes(name) && field.default === undefined
            const constrained = field.type === 'boolean' || field.enum
            const numeric = field.type === 'number' || field.type === 'integer'
            const placeholder =
              field.default === undefined ? '' : String(field.default)
            return (
              <label className='grid gap-1 text-sm' key={name}>
                {field.title || name}
                {required ? ' *' : ''}
                {constrained ? (
                  <select
                    className={selectClass}
                    value={value === undefined ? '' : JSON.stringify(value)}
                    onChange={(e) =>
                      changeParameter(
                        name,
                        e.target.value ? JSON.parse(e.target.value) : undefined
                      )
                    }
                  >
                    <option value=''>
                      {placeholder || t('Not configured')}
                    </option>
                    {(field.type === 'boolean'
                      ? [true, false]
                      : (field.enum ?? [])
                    ).map((option) => (
                      <option
                        key={String(option)}
                        value={JSON.stringify(option)}
                      >
                        {String(option)}
                      </option>
                    ))}
                  </select>
                ) : field.format === 'textarea' ? (
                  <Textarea
                    value={String(value ?? '')}
                    placeholder={placeholder}
                    onChange={(e) =>
                      changeParameter(name, e.target.value || undefined)
                    }
                  />
                ) : (
                  <Input
                    type={numeric ? 'number' : 'text'}
                    min={field.minimum}
                    max={field.maximum}
                    step={field.type === 'integer' ? 1 : 'any'}
                    value={String(value ?? '')}
                    placeholder={placeholder}
                    onChange={(e) => {
                      const next = e.target.value
                      changeParameter(
                        name,
                        next === ''
                          ? undefined
                          : numeric
                            ? e.target.valueAsNumber
                            : next
                      )
                    }}
                  />
                )}
                {field.description && (
                  <span className='text-muted-foreground text-xs'>
                    {field.description}
                  </span>
                )}
              </label>
            )
          })}
          {Object.keys(config)
            .filter((name) => !Object.hasOwn(schema?.properties ?? {}, name))
            .map((name) => (
              <Button
                type='button'
                variant='outline'
                key={name}
                onClick={() => changeParameter(name)}
              >
                {t('Remove unavailable parameter {{name}}', { name })}
              </Button>
            ))}
        </>
      )}
    </div>
  )
}
