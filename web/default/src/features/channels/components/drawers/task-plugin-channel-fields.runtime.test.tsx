// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import { act } from 'react'
import { FormProvider, useForm } from 'react-hook-form'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { I18nextProvider } from 'react-i18next'
import type { PluginEntry } from '@/features/task-plugins/api'
import { CHANNEL_FORM_DEFAULT_VALUES, type ChannelFormValues } from '../../lib'

const env = createReactTestEnvironment()
await env.setup()
const { render, fireEvent, cleanup } =
  await import('@testing-library/react/pure')
const { TaskPluginChannelFields } = await import('./task-plugin-channel-fields')
after(async () => {
  await act(async () => cleanup())
  env.teardown()
})

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
