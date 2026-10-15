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
import { act, useState } from 'react'
import { AxiosError } from 'axios'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import assert from 'node:assert/strict'
import { after, test } from 'node:test'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { useThemeRadiusPx } from '@/lib/theme-radius'
import type { UserProfile } from '@/features/profile/types'
import type { ChannelAffinitySettings } from '@/features/system-settings/general/channel-affinity/types'
import { useMinimumLoadingTime } from './use-minimum-loading-time'
import { useIsMobile } from './use-mobile'
import { useReadScope } from './use-read-scope'
import { useResettableState } from './use-resettable-state'

const env = createReactTestEnvironment()
await env.setup()
const { fireEvent, within } = await import('@testing-library/react/pure')
const { useProfile } = await import('@/features/profile/hooks/use-profile')
const { NotificationTab } =
  await import('@/features/profile/components/tabs/notification-tab')
const { ChannelAffinitySection } =
  await import('@/features/system-settings/general/channel-affinity')
const { SettingsPageProvider } =
  await import('@/features/system-settings/components/settings-page-context')
const { SidebarModulesCard } =
  await import('@/features/profile/components/sidebar-modules-card')
after(() => env.teardown())

test('sidebar settings recover malformed values and preserve explicit false without autosaving', async () => {
  const adapter = api.defaults.adapter,
    previousUser = useAuthStore.getState().auth.user
  const defaults = {
    chat: { enabled: true, playground: true, chat: true },
    console: {
      enabled: true,
      detail: true,
      token: true,
      log: true,
      midjourney: true,
      task: true,
    },
    personal: { enabled: true, topup: true, personal: true },
  }
  const valid = { chat: { enabled: false, chat: false } }
  const cases = ['{broken', 'null', '[]', 'false', '42', JSON.stringify(valid)]
  try {
    for (const [index, raw] of cases.entries()) {
      useAuthStore
        .getState()
        .auth.setUser({ id: 30 + index, username: 'sidebar-user', role: 1 })
      const saved: unknown[] = []
      api.defaults.adapter = async (config) => {
        assert.equal(config.url, '/api/user/self')
        if (config.method === 'put')
          saved.push(JSON.parse(JSON.parse(config.data).sidebar_modules))
        else assert.equal(config.method, 'get')
        return {
          config,
          status: 200,
          statusText: 'OK',
          headers: {},
          data: { success: true, data: { sidebar_modules: raw } },
        }
      }
      const view = await env.render(<SidebarModulesCard />)
      try {
        assert.equal(saved.length, 0)
        await view.click(
          within(view.container).getByRole('button', { name: 'Save Changes' })
        )
        assert.deepEqual(saved, [index === cases.length - 1 ? valid : defaults])
      } finally {
        await view.unmount()
      }
    }
  } finally {
    api.defaults.adapter = adapter
    useAuthStore.getState().auth.setUser(previousUser)
  }
})

test('channel affinity drafts survive equal saved values and reset together after a saved change', async () => {
  const adapter = api.defaults.adapter
  const requests: string[] = []
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const actions = env.dom.window.document.createElement('div')
  env.dom.window.document.body.append(actions)
  const initial: ChannelAffinitySettings = {
    'channel_affinity_setting.enabled': true,
    'channel_affinity_setting.switch_on_success': true,
    'channel_affinity_setting.keep_on_channel_disabled': false,
    'channel_affinity_setting.max_entries': 100,
    'channel_affinity_setting.default_ttl_seconds': 60,
    'channel_affinity_setting.rules': '[]',
  }
  api.defaults.adapter = async (config) => {
    requests.push(config.method || '')
    assert.equal(config.method, 'get')
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true, data: null },
    }
  }
  function Harness() {
    const [saved, setSaved] = useState(initial)
    const [, setRevision] = useState(0)
    return (
      <>
        <button onClick={() => setRevision((value) => value + 1)}>
          Refresh affinity host
        </button>
        <button
          onClick={() =>
            setSaved({
              ...initial,
              'channel_affinity_setting.max_entries': 222,
              'channel_affinity_setting.rules': '[{"name":"saved-rule"}]',
            })
          }
        >
          Change saved affinity
        </button>
        <ChannelAffinitySection defaultValues={{ ...saved }} />
      </>
    )
  }
  const view = await env.render(
    <QueryClientProvider client={client}>
      <SettingsPageProvider actionsContainer={actions}>
        <Harness />
      </SettingsPageProvider>
    </QueryClientProvider>
  )
  const fields = within(view.container),
    controls = within(actions)
  const switches = () => fields.getAllByRole('switch')
  const amounts = () => fields.getAllByRole('spinbutton') as HTMLInputElement[]
  try {
    for (const field of switches()) await view.click(field)
    await act(async () => {
      fireEvent.change(amounts()[0], { target: { value: '0' } })
      fireEvent.change(amounts()[1], { target: { value: '0' } })
    })
    await view.click(controls.getByRole('button', { name: 'JSON' }))
    const draft = '[{"name":"unsaved-rule","ttl_seconds":0}]'
    await act(async () => {
      fireEvent.change(fields.getByRole('textbox'), {
        target: { value: draft },
      })
    })
    await view.click(fields.getByText('Refresh affinity host'))
    assert.deepEqual(
      switches().map((field) => field.getAttribute('aria-checked')),
      ['false', 'false', 'true']
    )
    assert.deepEqual(
      amounts().map((field) => field.value),
      ['0', '0']
    )
    assert.equal(
      (fields.getByRole('textbox') as HTMLTextAreaElement).value,
      draft
    )
    await view.click(controls.getByRole('button', { name: 'Visual' }))
    await view.click(fields.getByText('Refresh affinity host'))
    assert.ok(fields.getByText('unsaved-rule'))
    await view.click(fields.getByText('Change saved affinity'))
    assert.deepEqual(
      switches().map((field) => field.getAttribute('aria-checked')),
      ['true', 'true', 'false']
    )
    assert.deepEqual(
      amounts().map((field) => field.value),
      ['222', '60']
    )
    assert.equal(fields.queryByText('unsaved-rule'), null)
    assert.ok(fields.getByText('saved-rule'))
    await view.click(controls.getByRole('button', { name: 'JSON' }))
    assert.deepEqual(
      JSON.parse((fields.getByRole('textbox') as HTMLTextAreaElement).value),
      [{ name: 'saved-rule' }]
    )
    assert.deepEqual(requests, ['get'])
  } finally {
    await view.unmount()
    actions.remove()
    client.clear()
    api.defaults.adapter = adapter
  }
})

test('notification draft survives an unchanged profile refresh and resets for saved settings or account', async () => {
  const adapter = api.defaults.adapter
  const writes: Array<{
    quota_warning_threshold?: number
    record_ip_log?: boolean
  }> = []
  let updated = 0
  const initial: UserProfile = {
    id: 1,
    username: 'notification-user',
    display_name: 'Notification user',
    role: 1,
    group: 'default',
    quota: 0,
    used_quota: 0,
    request_count: 0,
    status: 1,
    aff_count: 0,
    aff_quota: 0,
    aff_history_quota: 0,
    created_time: 0,
    setting: JSON.stringify({
      notify_type: 'email',
      quota_warning_threshold: 100,
      record_ip_log: false,
    }),
  }
  api.defaults.adapter = async (config) => {
    assert.equal(config.method, 'put')
    assert.equal(config.url, '/api/user/setting')
    writes.push(JSON.parse(config.data))
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: { success: true },
    }
  }
  function Harness() {
    const [profile, setProfile] = useState(initial)
    return (
      <>
        <button
          onClick={() =>
            setProfile((previous) => ({
              ...previous,
              quota: previous.quota + 1,
            }))
          }
        >
          Refresh same settings
        </button>
        <button
          onClick={() =>
            setProfile((previous) => ({
              ...previous,
              setting: JSON.stringify({
                notify_type: 'email',
                quota_warning_threshold: 10,
                record_ip_log: false,
              }),
            }))
          }
        >
          Change saved settings
        </button>
        <button
          onClick={() => setProfile((previous) => ({ ...previous, id: 2 }))}
        >
          Switch account
        </button>
        <button
          onClick={() =>
            setProfile((previous) => ({
              ...previous,
              setting: JSON.stringify({
                notify_type: 'email',
                quota_warning_threshold: 0,
                record_ip_log: false,
              }),
            }))
          }
        >
          Save explicit zero
        </button>
        <NotificationTab
          profile={profile}
          onUpdate={() => {
            updated += 1
          }}
        />
      </>
    )
  }
  const view = await env.render(<Harness />)
  const fields = within(view.container)
  const threshold = () =>
    fields.getByRole('spinbutton', {
      name: 'Quota Warning Threshold',
    }) as HTMLInputElement
  const editZero = () =>
    act(async () => {
      fireEvent.change(threshold(), { target: { value: '0' } })
    })
  try {
    assert.equal(threshold().value, '100')
    await editZero()
    assert.equal(threshold().value, '0')
    await view.click(fields.getByText('Refresh same settings'))
    assert.equal(threshold().value, '0')
    await view.click(fields.getByText('Change saved settings'))
    assert.equal(threshold().value, '10')
    await editZero()
    await view.click(fields.getByText('Switch account'))
    assert.equal(threshold().value, '10')
    await view.click(fields.getByText('Save explicit zero'))
    assert.equal(threshold().value, '0')
    await view.click(fields.getByRole('button', { name: 'Save Settings' }))
    assert.equal(writes.length, 1)
    assert.equal(writes[0].quota_warning_threshold, 0)
    assert.equal(writes[0].record_ip_log, false)
    assert.equal(updated, 1)
  } finally {
    await view.unmount()
    api.defaults.adapter = adapter
  }
})

test('failed settings writes do not log credential-bearing request bodies or retry', async () => {
  const adapter = api.defaults.adapter
  const originalLog = Object.getOwnPropertyDescriptor(console, 'error')!
  const logs: unknown[][] = []
  const marker = 'synthetic-private-marker'
  let writes = 0
  api.defaults.adapter = async (config) => {
    if (config.method === 'get')
      return {
        config,
        status: 200,
        statusText: 'OK',
        headers: {},
        data: {
          success: true,
          data: { id: 1, username: 'logging-profile', role: 1 },
        },
      }
    writes += 1
    throw new AxiosError(
      'Synthetic update failure',
      'ERR_BAD_RESPONSE',
      config,
      undefined,
      { config, status: 500, statusText: 'Error', headers: {}, data: {} }
    )
  }
  Object.defineProperty(console, 'error', {
    ...originalLog,
    value: (...args: unknown[]) => logs.push(args),
  })
  function Probe() {
    const { updateSettings } = useProfile()
    const [result, setResult] = useState('pending')
    return (
      <>
        <output>{result}</output>
        <button
          onClick={async () =>
            setResult(String(await updateSettings({ webhook_secret: marker })))
          }
        >
          Save private setting
        </button>
      </>
    )
  }
  const view = await env.render(<Probe />)
  try {
    await view.click(within(view.container).getByText('Save private setting'))
    assert.equal(
      within(view.container).getByRole('status').textContent,
      'false'
    )
    assert.equal(writes, 1)
    assert.ok(logs.length > 0)
    assert.equal(JSON.stringify(logs).includes(marker), false)
  } finally {
    await view.unmount()
    Object.defineProperty(console, 'error', originalLog)
    api.defaults.adapter = adapter
  }
})

test('a silent refresh can finish a visible profile load without leaving its spinner stuck', async () => {
  const adapter = api.defaults.adapter
  let release!: () => void
  let requests = 0
  api.defaults.adapter = async (config) => {
    requests += 1
    await new Promise<void>((resolve) => {
      release = resolve
    })
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: {},
      data: {
        success: true,
        data: { id: 1, username: 'profile-ready', role: 1 },
      },
    }
  }
  function Probe() {
    const { loading, profile, refreshProfile } = useProfile()
    return (
      <>
        <output aria-label='Loading'>{String(loading)}</output>
        <output aria-label='Profile'>{profile?.username || '-'}</output>
        <button onClick={() => void refreshProfile()}>Silent refresh</button>
      </>
    )
  }
  const view = await env.render(<Probe />)
  const fields = within(view.container)
  try {
    assert.equal(
      fields.getByRole('status', { name: 'Loading' }).textContent,
      'true'
    )
    await view.click(fields.getByText('Silent refresh'))
    await act(async () => release())
    assert.equal(
      fields.getByRole('status', { name: 'Loading' }).textContent,
      'false'
    )
    assert.equal(
      fields.getByRole('status', { name: 'Profile' }).textContent,
      'profile-ready'
    )
    assert.equal(requests, 1)
  } finally {
    release?.()
    await view.unmount()
    api.defaults.adapter = adapter
  }
})

test('drafts keep local edits on rerender and reset explicit zero and false with their source', async () => {
  const oldSetters: Array<
    (value: { count: number; enabled: boolean }) => void
  > = []
  function Harness() {
    const [source, setSource] = useState(1)
    const [, setRevision] = useState(0)
    const [draft, setDraft] = useResettableState(
      { count: 0, enabled: false },
      source
    )
    return (
      <>
        <output>{JSON.stringify(draft)}</output>
        <button
          onClick={() => {
            oldSetters.push(setDraft)
            setDraft({ count: 7, enabled: true })
          }}
        >
          Edit
        </button>
        <button onClick={() => setRevision((value) => value + 1)}>
          Render again
        </button>
        <button onClick={() => setSource((value) => value + 1)}>
          Change source
        </button>
        <button onClick={() => setSource(1)}>Reuse first source</button>
      </>
    )
  }
  const view = await env.render(<Harness />)
  const fields = within(view.container)
  try {
    await view.click(fields.getByText('Edit'))
    await view.click(fields.getByText('Render again'))
    assert.equal(
      fields.getByRole('status').textContent,
      '{"count":7,"enabled":true}'
    )
    await view.click(fields.getByText('Change source'))
    assert.equal(
      fields.getByRole('status').textContent,
      '{"count":0,"enabled":false}'
    )
    await view.click(fields.getByText('Reuse first source'))
    await act(async () => oldSetters[0]({ count: 99, enabled: true }))
    assert.equal(
      fields.getByRole('status').textContent,
      '{"count":0,"enabled":false}'
    )
  } finally {
    await view.unmount()
  }
})

test('read scopes reject older requests, changed entities, sessions and unmounted results', async () => {
  const previousUser = useAuthStore.getState().auth.user
  const reads: Array<() => boolean> = []
  const starts: Array<() => () => boolean> = []
  function Harness() {
    const [key, setKey] = useState(1)
    const begin = useReadScope(key)
    return (
      <>
        <button
          onClick={() => {
            starts.push(begin)
            reads.push(begin())
          }}
        >
          Read
        </button>
        <button onClick={() => setKey((value) => value + 1)}>Next</button>
        <button onClick={() => setKey(1)}>First again</button>
      </>
    )
  }
  const view = await env.render(<Harness />)
  const fields = within(view.container)
  try {
    await view.click(fields.getByText('Read'))
    await view.click(fields.getByText('Read'))
    assert.equal(reads[0](), false)
    assert.equal(reads[1](), true)
    await view.click(fields.getByText('Next'))
    assert.equal(reads[1](), false)
    await view.click(fields.getByText('Read'))
    assert.equal(reads[2](), true)
    assert.equal(starts[0]()(), false)
    assert.equal(reads[2](), true)
    await view.click(fields.getByText('First again'))
    await view.click(fields.getByText('Read'))
    assert.equal(starts[0]()(), false)
    assert.equal(reads[3](), true)
    await act(async () => useAuthStore.getState().auth.reset())
    assert.equal(reads[3](), false)
    await view.click(fields.getByText('Read'))
    await view.unmount()
    assert.equal(reads[4](), false)
  } finally {
    useAuthStore.getState().auth.setUser(previousUser)
  }
})

test('mobile snapshot starts at the current width and unsubscribes on unmount', async () => {
  const original = env.dom.window.matchMedia
  const width = Object.getOwnPropertyDescriptor(env.dom.window, 'innerWidth')
  const listeners = new Set<() => void>()
  let currentWidth = 500
  Object.defineProperty(env.dom.window, 'innerWidth', {
    configurable: true,
    get: () => currentWidth,
  })
  Object.defineProperty(env.dom.window, 'matchMedia', {
    configurable: true,
    value: () => ({
      addEventListener: (_event: string, listener: () => void) =>
        listeners.add(listener),
      removeEventListener: (_event: string, listener: () => void) =>
        listeners.delete(listener),
    }),
  })
  function Probe() {
    return <output>{String(useIsMobile())}</output>
  }
  const view = await env.render(<Probe />)
  try {
    assert.equal(view.container.textContent, 'true')
    await act(async () => {
      currentWidth = 900
      listeners.forEach((notify) => notify())
    })
    assert.equal(view.container.textContent, 'false')
    await view.unmount()
    assert.equal(listeners.size, 0)
  } finally {
    Object.defineProperty(env.dom.window, 'matchMedia', {
      configurable: true,
      value: original,
    })
    if (width) Object.defineProperty(env.dom.window, 'innerWidth', width)
  }
})

test('minimum loading lasts for the current cycle and old timers cannot end a newer cycle', async () => {
  const descriptors = new Map(
    ['setTimeout', 'clearTimeout'].map((key) => [
      key,
      Object.getOwnPropertyDescriptor(globalThis, key)!,
    ])
  )
  const originalNow = Date.now
  const timers = new Map<number, { at: number; run: () => void }>()
  let now = 0,
    nextId = 0
  Date.now = () => now
  Object.defineProperty(globalThis, 'setTimeout', {
    configurable: true,
    value: (run: () => void, delay = 0) => {
      const id = ++nextId
      timers.set(id, { at: now + delay, run })
      return id
    },
  })
  Object.defineProperty(globalThis, 'clearTimeout', {
    configurable: true,
    value: (id: number) => timers.delete(id),
  })
  const advance = async (milliseconds: number) =>
    act(async () => {
      now += milliseconds
      for (const [id, timer] of [...timers])
        if (timer.at <= now) {
          timers.delete(id)
          timer.run()
        }
    })
  function Harness() {
    const [loading, setLoading] = useState(false)
    const visible = useMinimumLoadingTime(loading, 100)
    return (
      <>
        <output>{String(visible)}</output>
        <button onClick={() => setLoading(true)}>Start</button>
        <button onClick={() => setLoading(false)}>Stop</button>
      </>
    )
  }
  const view = await env.render(<Harness />)
  const fields = within(view.container)
  const visible = () => fields.getByRole('status').textContent
  try {
    assert.equal(visible(), 'false')
    await view.click(fields.getByText('Start'))
    await advance(40)
    await view.click(fields.getByText('Stop'))
    assert.equal(visible(), 'true')
    await advance(60)
    assert.equal(visible(), 'false')
    await view.click(fields.getByText('Start'))
    await advance(100)
    assert.equal(visible(), 'true')
    await view.click(fields.getByText('Stop'))
    assert.equal(visible(), 'false')
    await view.click(fields.getByText('Start'))
    await advance(20)
    await view.click(fields.getByText('Stop'))
    await view.click(fields.getByText('Start'))
    await view.click(fields.getByText('Stop'))
    await advance(80)
    assert.equal(visible(), 'true')
    await advance(20)
    assert.equal(visible(), 'false')
  } finally {
    await view.unmount()
    assert.equal(timers.size, 0)
    Date.now = originalNow
    for (const [key, descriptor] of descriptors)
      Object.defineProperty(globalThis, key, descriptor)
  }
})

test('theme radius follows DOM changes and a refresh key without leaving resize listeners', async () => {
  const computed = Object.getOwnPropertyDescriptor(
    globalThis,
    'getComputedStyle'
  )!
  let radius = 12,
    measurements = 0
  Object.defineProperty(globalThis, 'getComputedStyle', {
    configurable: true,
    value: () => {
      measurements += 1
      return { borderTopLeftRadius: `${radius}px` }
    },
  })
  function Harness() {
    const [revision, setRevision] = useState(0)
    const value = useThemeRadiusPx('--radius-md', String(revision))
    return (
      <>
        <output>{String(value)}</output>
        <button onClick={() => setRevision((value) => value + 1)}>
          Refresh
        </button>
      </>
    )
  }
  const view = await env.render(<Harness />)
  try {
    assert.equal(within(view.container).getByRole('status').textContent, '12')
    await act(async () => {
      radius = 24
      env.dom.window.document.documentElement.setAttribute(
        'data-theme',
        'test-dark'
      )
    })
    assert.equal(within(view.container).getByRole('status').textContent, '24')
    radius = 36
    await view.click(within(view.container).getByText('Refresh'))
    assert.equal(within(view.container).getByRole('status').textContent, '36')
    await view.unmount()
    const before = measurements
    env.dom.window.dispatchEvent(new env.dom.window.Event('resize'))
    assert.equal(measurements, before)
  } finally {
    Object.defineProperty(globalThis, 'getComputedStyle', computed)
  }
})
