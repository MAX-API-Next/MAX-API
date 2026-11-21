// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { act } from 'react'
import { FormProvider, useForm } from 'react-hook-form'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, afterEach, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import type { PluginEntry } from '@/features/task-plugins/api'
import { CHANNEL_FORM_DEFAULT_VALUES, type ChannelFormValues } from '../../lib'
import { missingTaskPluginParameters } from '../../lib/task-plugin-channel'

const env = createReactTestEnvironment()
await env.setup()
const { render, fireEvent, cleanup } =
  await import('@testing-library/react/pure')
const { TaskPluginChannelFields } = await import('./task-plugin-channel-fields')
const { ChannelBasicSection } = await import('./sections/channel-basic-section')
afterEach(async () => act(async () => cleanup()))
after(() => env.teardown())

const plugin: PluginEntry = {
  key: 'fixture',
  name: 'Fixture',
  version: '1.0.0',
  digest: 'digest-one',
  format_version: 3,
  active: true,
  enabled: true,
  revoked: false,
  models: ['declared-model'],
  protocols: [],
  routes: [],
  usage_schema: {},
  usage_profiles: [],
  execution_ready: false,
  admission_ready: false,
  channel_configurable: true,
  base_url: 'https://provider.example',
  upstreams: ['vendor'],
  channel_config: {
    properties: {
      retries: { type: 'integer', default: 2 },
      flag: { type: 'boolean', default: true },
    },
  },
}

function Fixture(props: { options: PluginEntry[] }) {
  const form = useForm<ChannelFormValues>({
    defaultValues: CHANNEL_FORM_DEFAULT_VALUES,
  })
  const formOutput = form.watch()
  return (
    <I18nextProvider i18n={env.i18n}>
      <FormProvider {...form}>
        <TaskPluginChannelFields
          options={props.options}
          loading={false}
          failed={false}
          refresh={() => undefined}
        />
        <output aria-label='Form values'>{JSON.stringify(formOutput)}</output>
      </FormProvider>
    </I18nextProvider>
  )
}

test('chooses a declared plugin, retains zero and false, and requires reviewing a changed version', () => {
  const view = render(<Fixture options={[plugin]} />)
  fireEvent.change(view.getByLabelText('Task Plugin'), {
    target: { value: plugin.key },
  })
  fireEvent.change(view.getByLabelText('retries'), { target: { value: '0' } })
  fireEvent.change(view.getByLabelText('flag'), { target: { value: 'false' } })
  const data = () =>
    JSON.parse(view.getByLabelText('Form values').textContent ?? '')
  assert.equal(data().base_url, plugin.base_url)
  assert.equal(data().expected_task_plugin_digest, plugin.digest)
  assert.deepEqual(JSON.parse(data().setting).task_plugin_config, {
    retries: 0,
    flag: false,
  })
  const next = { ...plugin, version: '2.0.0', digest: 'digest-two' }
  view.rerender(<Fixture options={[next]} />)
  assert.match(
    view.getByRole('alert').textContent ?? '',
    /active plugin version changed/
  )
  assert.equal(data().expected_task_plugin_digest, plugin.digest)
  fireEvent.click(view.getByRole('button', { name: 'Use active version' }))
  assert.equal(data().expected_task_plugin_digest, next.digest)
  view.rerender(<Fixture options={[]} />)
  assert.match(
    view.getByRole('alert').textContent ?? '',
    /saved task plugin is unavailable/
  )
  assert.equal(JSON.parse(data().setting).task_plugin_key, plugin.key)
  assert.deepEqual(JSON.parse(data().setting).task_plugin_config, {
    retries: 0,
    flag: false,
  })
})

test('shows a removed upstream until a declared replacement is explicitly selected', () => {
  const view = render(<Fixture options={[plugin]} />)
  fireEvent.change(view.getByLabelText('Task Plugin'), {
    target: { value: plugin.key },
  })
  const next: PluginEntry = {
    ...plugin,
    digest: 'digest-two',
    upstreams: ['new_api'],
  }
  view.rerender(<Fixture options={[next]} />)
  fireEvent.click(view.getByRole('button', { name: 'Use active version' }))
  const select = view.getByLabelText('Upstream kind') as HTMLSelectElement
  assert.equal(select.value, 'vendor')
  assert.equal(select.selectedOptions[0].disabled, true)
  fireEvent.change(select, { target: { value: 'new_api' } })
  const data = JSON.parse(view.getByLabelText('Form values').textContent ?? '')
  assert.equal(JSON.parse(data.setting).task_plugin_upstream, 'new_api')
  assert.equal(data.expected_task_plugin_digest, next.digest)
})

test('rejects the reserved plugin type typed into an existing channel selector', () => {
  function ExistingChannel() {
    const form = useForm<ChannelFormValues>({
      defaultValues: { ...CHANNEL_FORM_DEFAULT_VALUES, type: 1 },
    })
    return (
      <I18nextProvider i18n={env.i18n}>
        <FormProvider {...form}>
          <ChannelBasicSection
            channelTypeOptions={[{ value: '1', label: 'OpenAI' }]}
            isEditing
            allowTaskPluginType={false}
          />
          <output aria-label='Channel type'>{form.watch('type')}</output>
        </FormProvider>
      </I18nextProvider>
    )
  }
  const view = render(<ExistingChannel />)
  const input = view.getByRole('combobox')
  fireEvent.change(input, { target: { value: '61' } })
  fireEvent.keyDown(input, { key: 'Enter' })
  assert.equal(view.getByLabelText('Channel type').textContent, '1')
  fireEvent.change(input, { target: { value: '1000' } })
  assert.equal(view.getByLabelText('Channel type').textContent, '1000')
})

test('switching a new batch draft to Task Plugin clears the batch credentials and mode', () => {
  function BatchChannel() {
    const form = useForm<ChannelFormValues>({
      defaultValues: {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        type: 1,
        multi_key_mode: 'multi_to_single',
        key: 'fixture-one\nfixture-two',
      },
    })
    const values = form.watch()
    return (
      <I18nextProvider i18n={env.i18n}>
        <FormProvider {...form}>
          <ChannelBasicSection
            channelTypeOptions={[
              { value: '1', label: 'OpenAI' },
              { value: '61', label: 'Task Plugin' },
            ]}
            isEditing={false}
            allowTaskPluginType
          />
          <output aria-label='Channel draft'>
            {JSON.stringify([values.type, values.multi_key_mode, values.key])}
          </output>
        </FormProvider>
      </I18nextProvider>
    )
  }
  const view = render(<BatchChannel />)
  fireEvent.change(view.getByRole('combobox'), { target: { value: '61' } })
  assert.deepEqual(
    JSON.parse(view.getByLabelText('Channel draft').textContent ?? ''),
    [61, 'single', '']
  )
})

test('requires unset plugin parameters and accepts zero, false and declared defaults', () => {
  const schema: NonNullable<PluginEntry['channel_config']> = {
    required: ['retries', 'flag', 'region', 'withDefault'],
    properties: {
      retries: { type: 'integer' },
      flag: { type: 'boolean' },
      region: { type: 'string', format: 'textarea' },
      withDefault: { type: 'integer', default: 0 },
    },
  }
  const view = render(
    <Fixture options={[{ ...plugin, channel_config: schema }]} />
  )
  fireEvent.change(view.getByLabelText('Task Plugin'), {
    target: { value: plugin.key },
  })
  for (const name of ['retries', 'flag', 'region']) {
    assert.equal(
      view.getByLabelText(name + ' *').hasAttribute('required'),
      true
    )
  }
  assert.equal(
    view.getByLabelText('withDefault').hasAttribute('required'),
    false
  )
  assert.deepEqual(missingTaskPluginParameters(schema, {}), [
    'retries',
    'flag',
    'region',
  ])
  assert.deepEqual(
    missingTaskPluginParameters(schema, {
      retries: 0,
      flag: false,
      region: '',
    }),
    ['region']
  )
  assert.deepEqual(
    missingTaskPluginParameters(schema, {
      retries: 0,
      flag: false,
      region: 'us',
    }),
    []
  )
})
