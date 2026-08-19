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
import { act } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createReactTestEnvironment } from '@/test/react'
import { waitFor } from '@testing-library/react'
import assert from 'node:assert/strict'
import { after, before, describe, test } from 'node:test'
import { api } from '@/lib/api'
import { formatQuota } from '@/lib/format'
import { ProductionPerformance } from './index'
import { getChannelDetailObservation } from './lib/channel-detail'
import type { ChannelPerformanceData } from './types'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

after(() => testEnv.teardown())

const emptyPerformanceData: ChannelPerformanceData = {
  storage_mode: 'legacy_log',
  source: 'log_db',
  partial: true,
  quality_flags: ['legacy'],
  time_range: { start_at: 1, end_at: 3601, hours: 1 },
  summary: {
    channel_count: 0,
    observed_count: 0,
    consume_log_count: 0,
    error_log_count: 0,
    consumed_quota: 0,
    retry_log_count: 0,
    latency_sample_count: 0,
    observed_success_rate: null,
    avg_logged_latency_ms: null,
    last_observed_at: 0,
  },
  items: [],
  truncated: false,
  generated_at: 3601,
}

const performanceDataWithErrors: ChannelPerformanceData = {
  ...emptyPerformanceData,
  summary: {
    ...emptyPerformanceData.summary,
    channel_count: 1,
    observed_count: 10,
    consume_log_count: 7,
    error_log_count: 3,
    consumed_quota: 750000,
    observed_success_rate: 70,
    last_observed_at: 3600,
  },
  items: [
    {
      channel_id: 12,
      channel_name: 'Test channel',
      channel_type: 1,
      channel_status: 1,
      model_name: 'test-model',
      effective_group: 'default',
      observed_count: 10,
      consume_log_count: 7,
      error_log_count: 3,
      consumed_quota: 750000,
      retry_log_count: 0,
      latency_sample_count: 7,
      observed_success_rate: 70,
      avg_logged_latency_ms: 1000,
      avg_tps: null,
      last_observed_at: 3600,
      probe_latency_ms: 100,
      probe_test_time: 3500,
      quality_flags: ['legacy'],
    },
  ],
}

const performanceDataWithDisabledErrorLogs: ChannelPerformanceData = {
  ...performanceDataWithErrors,
  quality_flags: [
    'legacy',
    'partial',
    'weak_correlation',
    'heuristic_outcome',
    'coarse_latency',
    'probe_separate',
    'non_attempt_logs_excluded',
    'incomplete_errors_possible',
    'error_logs_disabled',
  ],
  summary: {
    ...performanceDataWithErrors.summary,
    error_log_count: 0,
    observed_success_rate: null,
  },
  items: performanceDataWithErrors.items.map((item) => ({
    ...item,
    error_log_count: 0,
    observed_success_rate: null,
    quality_flags: [
      ...item.quality_flags,
      'incomplete_errors_possible',
      'error_logs_disabled',
    ],
  })),
}

const performanceDataWithSortableRows: ChannelPerformanceData = {
  ...performanceDataWithErrors,
  items: [
    {
      ...performanceDataWithErrors.items[0],
      channel_id: 21,
      channel_name: 'Healthy channel',
      observed_count: 20,
      consume_log_count: 20,
      error_log_count: 0,
      consumed_quota: 1000000,
      observed_success_rate: 100,
      avg_logged_latency_ms: 300,
      retry_log_count: 0,
      probe_latency_ms: 80,
    },
    {
      ...performanceDataWithErrors.items[0],
      channel_id: 22,
      channel_name: 'Problem channel',
      observed_count: 12,
      consume_log_count: 4,
      error_log_count: 8,
      consumed_quota: 200000,
      observed_success_rate: 33.333,
      avg_logged_latency_ms: 5000,
      retry_log_count: 6,
      probe_latency_ms: 1200,
    },
    {
      ...performanceDataWithErrors.items[0],
      channel_id: 23,
      channel_name: 'Unknown channel',
      observed_count: 2,
      consume_log_count: 2,
      error_log_count: 0,
      consumed_quota: 0,
      observed_success_rate: null,
      avg_logged_latency_ms: null,
      retry_log_count: null,
      probe_latency_ms: null,
    },
  ],
}

const channelDetailData: ChannelPerformanceData = {
  ...performanceDataWithErrors,
  quality_flags: ['error_logs_disabled'],
  time_range: { start_at: 1, end_at: 86401, hours: 24 },
  summary: {
    ...performanceDataWithErrors.summary,
    observed_count: 15,
    consume_log_count: 12,
    error_log_count: 3,
    consumed_quota: 900000,
    observed_success_rate: 80,
    last_observed_at: 7200,
  },
  items: [
    performanceDataWithErrors.items[0],
    {
      ...performanceDataWithErrors.items[0],
      model_name: 'second-model',
      effective_group: 'premium',
      observed_count: 5,
      consume_log_count: 5,
      error_log_count: 0,
      consumed_quota: 150000,
      observed_success_rate: 100,
    },
  ],
}

describe('ProductionPerformance manual queries', () => {
  test('requests data only after explicit user actions', async () => {
    const originalGet = api.get
    let requestCount = 0
    api.get = (async () => {
      requestCount += 1
      return { data: { success: true, data: emptyPerformanceData } }
    }) as typeof api.get

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ProductionPerformance />
      </QueryClientProvider>
    )

    try {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      assert.equal(requestCount, 0, 'mounting the page must not query logs')

      const findButton = (label: string) => {
        const button = Array.from(
          view.container.querySelectorAll('button')
        ).find((candidate) => candidate.textContent?.trim() === label)
        assert.ok(button, `expected ${label} button`)
        return button
      }

      await view.click(findButton('Apply filters'))
      await waitFor(() => assert.equal(requestCount, 1))

      await view.click(findButton('Apply filters'))
      await waitFor(() => assert.equal(requestCount, 2))

      await act(async () => {
        window.dispatchEvent(new Event('focus'))
        window.dispatchEvent(new Event('online'))
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      assert.equal(requestCount, 2, 'focus and reconnect must not query logs')

      await view.click(findButton('Refresh'))
      await waitFor(() => assert.equal(requestCount, 3))
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('shows error counts and consumed quota in the channel performance table', async () => {
    const originalGet = api.get
    api.get = (async () => ({
      data: { success: true, data: performanceDataWithErrors },
    })) as typeof api.get

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ProductionPerformance />
      </QueryClientProvider>
    )

    try {
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')

      await view.click(applyButton)
      await waitFor(() => {
        assert.ok(
          Array.from(
            view.container.querySelectorAll('[data-slot="card-description"]')
          ).some((node) => node.textContent?.trim() === 'Recorded error logs')
        )
      })

      const errorCardLabel = Array.from(
        view.container.querySelectorAll('[data-slot="card-description"]')
      ).find((node) => node.textContent?.trim() === 'Recorded error logs')
      assert.ok(errorCardLabel, 'expected error summary card')
      assert.equal(
        errorCardLabel.parentElement?.querySelector('[data-slot="card-title"]')
          ?.textContent,
        '3'
      )

      const headers = Array.from(view.container.querySelectorAll('thead th'))
      const errorColumnIndex = headers.findIndex(
        (header) => header.textContent?.trim() === 'Recorded error logs'
      )
      assert.ok(errorColumnIndex >= 0, 'expected error count table column')
      const rowCells = view.container.querySelectorAll(
        'tbody tr:first-child td'
      )
      assert.equal(rowCells.item(errorColumnIndex).textContent?.trim(), '3')
      const consumedQuotaColumnIndex = headers.findIndex(
        (header) => header.textContent?.trim() === 'Consumed quota'
      )
      assert.ok(consumedQuotaColumnIndex >= 0, 'expected consumed quota column')
      assert.equal(
        rowCells.item(consumedQuotaColumnIndex).textContent?.trim(),
        formatQuota(750000)
      )
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('keeps recorded errors visible and warns when error logging is disabled', async () => {
    const originalGet = api.get
    api.get = (async () => ({
      data: {
        success: true,
        data: performanceDataWithDisabledErrorLogs,
      },
    })) as typeof api.get

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ProductionPerformance />
      </QueryClientProvider>
    )

    try {
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')

      await view.click(applyButton)
      await waitFor(() => {
        assert.ok(
          Array.from(
            view.container.querySelectorAll('[data-slot="alert-title"]')
          ).some(
            (node) =>
              node.textContent?.trim() === 'Error log collection is disabled'
          )
        )
      })

      const errorCardLabel = Array.from(
        view.container.querySelectorAll('[data-slot="card-description"]')
      ).find((node) => node.textContent?.trim() === 'Recorded error logs')
      assert.ok(errorCardLabel, 'expected recorded error summary card')
      assert.equal(
        errorCardLabel.parentElement?.querySelector('[data-slot="card-title"]')
          ?.textContent,
        '0'
      )

      const successCardLabel = Array.from(
        view.container.querySelectorAll('[data-slot="card-description"]')
      ).find((node) => node.textContent?.trim() === 'Estimated success rate')
      assert.ok(successCardLabel, 'expected success-rate summary card')
      assert.equal(
        successCardLabel.parentElement?.querySelector(
          '[data-slot="card-title"]'
        )?.textContent,
        'N/A'
      )

      assert.ok(
        Array.from(view.container.querySelectorAll('[data-slot="badge"]')).some(
          (node) => node.textContent?.trim() === 'Error logs disabled'
        ),
        'critical log collection state must not be hidden by badge truncation'
      )
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('provides a semantic details button without making the table row tabbable', async () => {
    const originalGet = api.get
    api.get = (async () => ({
      data: { success: true, data: performanceDataWithErrors },
    })) as typeof api.get

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ProductionPerformance />
      </QueryClientProvider>
    )

    try {
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')
      await view.click(applyButton)

      const detailsButton = await waitFor(() => {
        const button = Array.from(
          view.container.querySelectorAll<HTMLButtonElement>('tbody button')
        ).find((candidate) => candidate.textContent?.trim() === 'Details')
        assert.ok(button, 'expected semantic details button')
        return button
      })
      assert.equal(
        detailsButton.getAttribute('aria-label'),
        'View details: Test channel · test-model'
      )
      assert.equal(
        detailsButton.closest('tr')?.getAttribute('tabindex'),
        null,
        'the row must not compete with the semantic button in keyboard order'
      )
      assert.equal(detailsButton.tagName, 'BUTTON')
      assert.equal(detailsButton.getAttribute('type'), 'button')
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('loads a channel-scoped 24-hour detail only when the detail sheet opens', async () => {
    const originalGet = api.get
    const urls: string[] = []
    api.get = (async (url, config) => {
      urls.push(String(url))
      if (url === '/api/smart-ops/channel-performance/detail') {
        assert.deepEqual(config?.params, { channel_id: 12 })
        return { data: { success: true, data: channelDetailData } }
      }
      return { data: { success: true, data: performanceDataWithErrors } }
    }) as typeof api.get

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ProductionPerformance />
      </QueryClientProvider>
    )

    try {
      assert.deepEqual(urls, [])
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton)
      await view.click(applyButton)
      await waitFor(() => assert.equal(urls.length, 1))
      assert.equal(urls[0], '/api/smart-ops/channel-performance')

      const detailsButton = await waitFor(() => {
        const button = Array.from(
          view.container.querySelectorAll<HTMLButtonElement>('tbody button')
        ).find((candidate) => candidate.textContent?.trim() === 'Details')
        assert.ok(button)
        return button
      })
      await view.click(detailsButton)

      await waitFor(() => {
        assert.equal(urls.length, 2)
        assert.equal(urls[1], '/api/smart-ops/channel-performance/detail')
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('uses channel-scoped detail evidence instead of the clicked list row', () => {
    const observation = getChannelDetailObservation(
      performanceDataWithErrors.items[0],
      channelDetailData
    )

    assert.equal(
      observation.lastObservedAt,
      channelDetailData.summary.last_observed_at
    )
    assert.deepEqual(observation.qualityFlags, channelDetailData.quality_flags)
  })

  test('exposes an accessible sort menu for every performance metric', async () => {
    const originalGet = api.get
    api.get = (async () => ({
      data: { success: true, data: performanceDataWithSortableRows },
    })) as typeof api.get

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    })
    const view = await testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ProductionPerformance />
      </QueryClientProvider>
    )

    try {
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')
      await view.click(applyButton)

      const sortableLabels = [
        'Observed log events',
        'Recorded error logs',
        'Consumed quota',
        'Estimated success rate',
        'Logged latency',
        'Retries',
        'Probe latency',
      ]
      const headerButtons = await waitFor(() => {
        const buttons = Array.from(
          view.container.querySelectorAll('thead button')
        )
        for (const label of sortableLabels) {
          assert.ok(
            buttons.some((button) => button.textContent?.trim() === label),
            `expected sortable ${label} header`
          )
        }
        return buttons
      })

      const successRateHeader = headerButtons.find(
        (button) => button.textContent?.trim() === 'Estimated success rate'
      )
      assert.ok(successRateHeader, 'expected success-rate sort trigger')
      for (const button of headerButtons) {
        if (!sortableLabels.includes(button.textContent?.trim() ?? '')) continue
        assert.equal(button.getAttribute('aria-haspopup'), 'menu')
        assert.equal(button.closest('th')?.getAttribute('aria-sort'), 'none')
      }
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })
})
