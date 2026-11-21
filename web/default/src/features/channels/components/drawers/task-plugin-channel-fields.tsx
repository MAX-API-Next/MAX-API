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
import {
  readTaskPluginSettings,
  taskPluginUpstreams,
} from '../../lib/task-plugin-channel'

const selectClass = 'bg-background w-full rounded-md border p-2'

type ParameterValue = string | number | boolean
type ParameterField = NonNullable<
  PluginEntry['channel_config']
>['properties'][string]

function ParameterInput(props: {
  field: ParameterField
  value?: ParameterValue
  onChange: (value?: ParameterValue) => void
}) {
  const { t } = useTranslation()
  const field = props.field
  const placeholder = field.default === undefined ? '' : String(field.default)
  if (field.type === 'boolean' || field.enum) {
    const options =
      field.type === 'boolean' ? [true, false] : (field.enum ?? [])
    return (
      <select
        className={selectClass}
        value={props.value === undefined ? '' : JSON.stringify(props.value)}
        onChange={(event) =>
          props.onChange(
            options.find(
              (option) => JSON.stringify(option) === event.target.value
            )
          )
        }
      >
        <option value=''>{placeholder || t('Not configured')}</option>
        {options.map((option) => (
          <option key={String(option)} value={JSON.stringify(option)}>
            {String(option)}
          </option>
        ))}
      </select>
    )
  }
  if (field.format === 'textarea') {
    return (
      <Textarea
        value={String(props.value ?? '')}
        placeholder={placeholder}
        onChange={(event) => props.onChange(event.target.value || undefined)}
      />
    )
  }
  const numeric = field.type === 'number' || field.type === 'integer'
  return (
    <Input
      type={numeric ? 'number' : 'text'}
      min={field.minimum}
      max={field.maximum}
      step={field.type === 'integer' ? 1 : 'any'}
      value={String(props.value ?? '')}
      placeholder={placeholder}
      onChange={(event) => {
        const { value, valueAsNumber } = event.target
        if (value === '') props.onChange()
        else if (numeric) props.onChange(valueAsNumber)
        else props.onChange(value)
      }}
    />
  )
}

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
  const upstreams = taskPluginUpstreams(selected?.upstreams)
  const upstreamUnavailable = !upstreams.includes(settings.task_plugin_upstream)
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
        task_plugin_upstream: taskPluginUpstreams(next?.upstreams)[0],
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

  function changeParameter(name: string, value?: ParameterValue) {
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
              aria-invalid={upstreamUnavailable}
              onChange={(e) => {
                setValue(
                  'setting',
                  JSON.stringify({
                    ...settings,
                    task_plugin_upstream: e.target.value,
                  }),
                  dirty
                )
                setValue('key', '', dirty)
              }}
            >
              {upstreamUnavailable && (
                <option value={settings.task_plugin_upstream} disabled>
                  {settings.task_plugin_upstream} ({t('Unavailable')})
                </option>
              )}
              {upstreams.map((kind) => (
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
            return (
              <label className='grid gap-1 text-sm' key={name}>
                {field.title || name}
                {required ? ' *' : ''}
                <ParameterInput
                  field={field}
                  value={value}
                  onChange={(next) => changeParameter(name, next)}
                />
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
