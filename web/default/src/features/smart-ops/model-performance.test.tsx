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
import { formatThroughput } from '@/features/performance-metrics/lib/format'
import type {
  PerformanceAggregate,
  PerformanceGroup,
} from '@/features/performance-metrics/types'
import {
  buildLatencyChartData,
  buildUptimeChartData,
  downsampleUptimeSeries,
  getUptimeAxisDomain,
} from '@/features/pricing/components/model-details-chart-utils'
import { ModelPerformanceDetailsContent } from '@/features/pricing/components/model-details-performance'
import { UptimeSparkline } from '@/features/pricing/components/model-details-uptime-sparkline'
import {
  ModelPerformance,
  ModelPerformanceDetailGrid,
} from './model-performance'
import type { ModelPerformanceData } from './types'

const testEnv = createReactTestEnvironment()

before(() => testEnv.setup())

after(() => testEnv.teardown())

const modelPerformanceData: ModelPerformanceData = {
  storage_mode: 'legacy_log',
  source: 'log_db',
  partial: true,
  quality_flags: ['legacy'],
  time_range: { start_at: 1, end_at: 3601, hours: 1 },
  throughput: {
    collection_state: 'available',
    coverage: {
      requested_start_at: 1,
      requested_end_at: 3601,
      bucket_start_at: 3600,
      bucket_end_at: 3601,
      bucket_seconds: 3600,
      granularity_state: 'unknown',
      approximate: true,
    },
  },
  summary: {
    model_count: 2,
    channel_count: 3,
    observed_count: 15,
    consume_log_count: 12,
    error_log_count: 3,
    consumed_quota: 1200000,
    retry_log_count: 2,
    latency_sample_count: 12,
    observed_success_rate: 80,
    avg_logged_latency_ms: 1200,
    last_observed_at: 3600,
  },
  items: [
    {
      model_name: 'alpha',
      channel_count: 2,
      observed_count: 10,
      consume_log_count: 8,
      error_log_count: 2,
      consumed_quota: 800000,
      retry_log_count: 1,
      latency_sample_count: 8,
      observed_success_rate: 80,
      avg_logged_latency_ms: 1000,
      avg_tps: 48.5,
      last_observed_at: 3600,
      quality_flags: ['legacy'],
    },
    {
      model_name: 'beta',
      channel_count: 1,
      observed_count: 5,
      consume_log_count: 4,
      error_log_count: 1,
      consumed_quota: 400000,
      retry_log_count: 1,
      latency_sample_count: 4,
      observed_success_rate: 80,
      avg_logged_latency_ms: 1600,
      avg_tps: 12.25,
      last_observed_at: 3500,
      quality_flags: ['legacy'],
    },
  ],
  truncated: false,
  generated_at: 3601,
}

function renderModelPerformance() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return {
    queryClient,
    render: testEnv.render(
      <QueryClientProvider client={queryClient}>
        <ModelPerformance />
      </QueryClientProvider>
    ),
  }
}

describe('ModelPerformance manual queries', () => {
  test('does not query on mount and sends the default hour range only after Apply filters', async () => {
    const originalGet = api.get
    let requestCount = 0
    let requestParams: Record<string, unknown> | undefined
    api.get = (async (_url, config) => {
      requestCount += 1
      requestParams = config?.params as Record<string, unknown>
      return { data: { success: true, data: modelPerformanceData } }
    }) as typeof api.get

    const { queryClient, render } = renderModelPerformance()
    const view = await render

    try {
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 0))
      })
      assert.equal(requestCount, 0, 'mounting must not query model logs')

      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')
      await view.click(applyButton)

      await waitFor(() => assert.equal(requestCount, 1))
      assert.equal(requestParams?.hours, 1)
      assert.equal(requestParams?.limit, 200)
      assert.equal(requestParams?.model, undefined)
      assert.equal(requestParams?.group, undefined)
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('renders every returned model with errors, consumed quota, and throughput', async () => {
    const originalGet = api.get
    api.get = (async () => ({
      data: { success: true, data: modelPerformanceData },
    })) as typeof api.get

    const { queryClient, render } = renderModelPerformance()
    const view = await render

    try {
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')
      await view.click(applyButton)

      await waitFor(() => {
        assert.equal(view.container.querySelectorAll('tbody tr').length, 2)
      })

      assert.ok(view.container.textContent?.includes('alpha'))
      assert.ok(view.container.textContent?.includes('beta'))
      const errorCardLabel = Array.from(
        view.container.querySelectorAll('[data-slot="card-description"]')
      ).find((node) => node.textContent?.trim() === 'Recorded error logs')
      assert.ok(errorCardLabel, 'expected recorded error summary card')
      assert.equal(
        errorCardLabel.parentElement?.querySelector('[data-slot="card-title"]')
          ?.textContent,
        '3'
      )
      const headers = Array.from(view.container.querySelectorAll('thead th'))
      const errorColumnIndex = headers.findIndex(
        (header) => header.textContent?.trim() === 'Recorded error logs'
      )
      assert.ok(errorColumnIndex >= 0, 'expected error count column')
      const rows = view.container.querySelectorAll('tbody tr')
      assert.equal(
        rows
          .item(0)
          .querySelectorAll('td')
          .item(errorColumnIndex)
          .textContent?.trim(),
        '2'
      )
      assert.equal(
        rows
          .item(1)
          .querySelectorAll('td')
          .item(errorColumnIndex)
          .textContent?.trim(),
        '1'
      )
      const consumedQuotaColumnIndex = headers.findIndex(
        (header) => header.textContent?.trim() === 'Consumed quota'
      )
      assert.ok(consumedQuotaColumnIndex >= 0, 'expected consumed quota column')
      assert.equal(
        rows
          .item(0)
          .querySelectorAll('td')
          .item(consumedQuotaColumnIndex)
          .textContent?.trim(),
        formatQuota(800000)
      )
      const throughputColumnIndex = headers.findIndex(
        (header) => header.textContent?.trim() === 'Throughput'
      )
      assert.ok(throughputColumnIndex >= 0, 'expected throughput column')
      assert.equal(
        rows
          .item(0)
          .querySelectorAll('td')
          .item(throughputColumnIndex)
          .textContent?.trim(),
        formatThroughput(48.5)
      )
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('exposes a sort menu for each model performance metric', async () => {
    const originalGet = api.get
    api.get = (async () => ({
      data: { success: true, data: modelPerformanceData },
    })) as typeof api.get

    const { queryClient, render } = renderModelPerformance()
    const view = await render

    try {
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')
      await view.click(applyButton)

      const sortableLabels = [
        'Model',
        'Channels',
        'Observed model log events',
        'Recorded error logs',
        'Consumed quota',
        'Estimated success rate',
        'Logged latency',
        'Throughput',
        'Retries',
      ]
      await waitFor(() => {
        const buttons = Array.from(
          view.container.querySelectorAll('thead button')
        )
        for (const label of sortableLabels) {
          assert.ok(
            buttons.some((button) => button.textContent?.trim() === label),
            `expected sortable ${label} header`
          )
        }
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('distinguishes disabled, empty, and failed throughput collection states', async () => {
    const originalGet = api.get
    const scenarios = [
      {
        state: 'collection_disabled' as const,
        flag: 'throughput_collection_disabled',
        title: 'Performance metrics collection is disabled',
      },
      {
        state: 'no_samples' as const,
        flag: 'throughput_no_samples',
        title: 'No throughput samples',
      },
      {
        state: 'query_failed' as const,
        flag: 'throughput_query_failed',
        title: 'Throughput query failed',
      },
    ]

    try {
      for (const scenario of scenarios) {
        api.get = (async () => ({
          data: {
            success: true,
            data: {
              ...modelPerformanceData,
              quality_flags: [scenario.flag],
              throughput: {
                ...modelPerformanceData.throughput,
                collection_state: scenario.state,
              },
            },
          },
        })) as typeof api.get

        const { queryClient, render } = renderModelPerformance()
        const view = await render
        try {
          const applyButton = Array.from(
            view.container.querySelectorAll('button')
          ).find(
            (candidate) => candidate.textContent?.trim() === 'Apply filters'
          )
          assert.ok(applyButton, 'expected Apply filters button')
          await view.click(applyButton)
          await waitFor(() => {
            assert.ok(view.container.textContent?.includes(scenario.title))
          })
        } finally {
          queryClient.clear()
          await view.unmount()
        }
      }
    } finally {
      api.get = originalGet
    }
  })

  test('does not claim a precise bucket size when historical granularity is unknown', async () => {
    const originalGet = api.get
    api.get = (async () => ({
      data: {
        success: true,
        data: {
          ...modelPerformanceData,
          quality_flags: ['throughput_window_approximate'],
        },
      },
    })) as typeof api.get

    const { queryClient, render } = renderModelPerformance()
    const view = await render

    try {
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')
      await view.click(applyButton)

      await waitFor(() => {
        const text = view.container.textContent ?? ''
        assert.ok(text.includes('Bucket granularity is unknown or mixed'))
        assert.ok(!text.includes('60 minute buckets'))
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('loads 24-hour model performance only after opening details', async () => {
    const originalGet = api.get
    let detailRequests = 0
    api.get = (async (url, config) => {
      if (url === '/api/smart-ops/model-performance/detail') {
        detailRequests += 1
        assert.equal(config?.params?.model, 'alpha')
        return {
          data: {
            success: true,
            data: {
              model_name: 'alpha',
              series_schema: 'test',
              collection_state: 'available',
              coverage: {
                requested_start_at: 1,
                requested_end_at: 3601,
                bucket_start_at: 3600,
                bucket_end_at: 3601,
                bucket_seconds: 3600,
                granularity_state: 'unknown',
                approximate: true,
              },
              summary: {
                avg_ttft_ms: 320,
                avg_latency_ms: 1200,
                success_rate: 99.5,
                avg_tps: 48.5,
                series: [
                  {
                    ts: 3600,
                    avg_ttft_ms: 320,
                    avg_latency_ms: 1200,
                    success_rate: 99.5,
                    avg_tps: 48.5,
                  },
                ],
              },
              groups: [
                {
                  group: 'default',
                  avg_ttft_ms: 320,
                  avg_latency_ms: 1200,
                  success_rate: 99.5,
                  avg_tps: 48.5,
                  series: [
                    {
                      ts: 3600,
                      avg_ttft_ms: 320,
                      avg_latency_ms: 1200,
                      success_rate: 99.5,
                      avg_tps: 48.5,
                    },
                  ],
                },
              ],
            },
          },
        }
      }
      return { data: { success: true, data: modelPerformanceData } }
    }) as typeof api.get

    const { queryClient, render } = renderModelPerformance()
    const view = await render

    try {
      const applyButton = Array.from(
        view.container.querySelectorAll('button')
      ).find((candidate) => candidate.textContent?.trim() === 'Apply filters')
      assert.ok(applyButton, 'expected Apply filters button')
      await view.click(applyButton)
      await waitFor(() => {
        assert.equal(view.container.querySelectorAll('tbody tr').length, 2)
      })
      assert.equal(detailRequests, 0, 'list loading must not fetch detail data')

      const detailsButton = Array.from(
        view.container.querySelectorAll<HTMLButtonElement>('tbody button')
      ).find((candidate) => candidate.textContent?.trim() === 'Details')
      assert.ok(detailsButton, 'expected model details button')
      await view.click(detailsButton)

      await waitFor(() => {
        assert.equal(detailRequests, 1)
      })
    } finally {
      api.get = originalGet
      queryClient.clear()
      await view.unmount()
    }
  })

  test('renders per-group performance and 24-hour trend sections', async () => {
    const groups: PerformanceGroup[] = [
      {
        group: 'default',
        avg_ttft_ms: 320,
        avg_latency_ms: 1200,
        success_rate: 99.5,
        avg_tps: 48.5,
        series: [
          {
            ts: 3600,
            avg_ttft_ms: 320,
            avg_latency_ms: 1200,
            success_rate: 99.5,
            avg_tps: 48.5,
          },
        ],
      },
    ]
    const summary: PerformanceAggregate = {
      avg_ttft_ms: 320,
      avg_latency_ms: 1200,
      success_rate: 99.5,
      avg_tps: 48.5,
      series: groups[0].series,
    }
    const view = await testEnv.render(
      <ModelPerformanceDetailsContent groups={groups} summary={summary} />
    )

    try {
      const detailText = view.container.textContent ?? ''
      assert.ok(detailText.includes('Per-group performance'))
      assert.ok(detailText.includes('Latency trend (last 24h)'))
      assert.ok(detailText.includes('Availability (last 24h)'))
      assert.ok(detailText.includes('default'))
      assert.ok(detailText.includes(formatThroughput(48.5)))
    } finally {
      await view.unmount()
    }
  })

  test('prevents the model detail summary grid from collapsing in the sheet', async () => {
    const view = await testEnv.render(
      <div className='flex h-0 flex-col overflow-hidden'>
        <ModelPerformanceDetailGrid rows={[['Model', 'alpha']]} />
      </div>
    )

    try {
      const detailGrid = view.container.querySelector('dl')
      assert.ok(detailGrid, 'expected model detail grid')
      assert.ok(
        detailGrid.classList.contains('shrink-0'),
        'model detail grid must not collapse inside a constrained flex sheet'
      )
    } finally {
      await view.unmount()
    }
  })

  test('uses the backend weighted model aggregate instead of averaging groups', async () => {
    const groups: PerformanceGroup[] = [
      {
        group: 'small',
        avg_ttft_ms: 1000,
        avg_latency_ms: 1000,
        success_rate: 0,
        avg_tps: 10,
        series: [
          {
            ts: 3600,
            avg_ttft_ms: 1000,
            avg_latency_ms: 1000,
            success_rate: 0,
            avg_tps: 10,
          },
        ],
      },
      {
        group: 'large',
        avg_ttft_ms: 50,
        avg_latency_ms: 100,
        success_rate: 100,
        avg_tps: 100,
        series: [
          {
            ts: 3600,
            avg_ttft_ms: 50,
            avg_latency_ms: 100,
            success_rate: 100,
            avg_tps: 100,
          },
        ],
      },
    ]
    const summary: PerformanceAggregate = {
      avg_ttft_ms: 50,
      avg_latency_ms: 109,
      success_rate: 99,
      avg_tps: 91.743,
      series: [],
    }
    const view = await testEnv.render(
      <ModelPerformanceDetailsContent groups={groups} summary={summary} />
    )

    try {
      const detailText = view.container.textContent ?? ''
      assert.ok(detailText.includes('99.00%'))
      assert.ok(detailText.includes(formatThroughput(91.743)))
      assert.ok(!detailText.includes('50.00%'))
    } finally {
      await view.unmount()
    }
  })

  test('expands the uptime axis to include severe availability failures', () => {
    assert.deepEqual(
      getUptimeAxisDomain([
        {
          date: new Date(3600 * 1000).toISOString(),
          uptime_pct: 4.11,
          incidents: 1,
          outage_minutes: 0,
        },
        {
          date: new Date(7200 * 1000).toISOString(),
          uptime_pct: 99.5,
          incidents: 1,
          outage_minutes: 0,
        },
      ]),
      { min: 0, max: 100 }
    )
  })

  test('keeps minute-level chart categories unique', () => {
    const first = new Date('2026-08-18T10:01:00.000Z').toISOString()
    const second = new Date('2026-08-18T10:02:00.000Z').toISOString()

    const latency = buildLatencyChartData([
      { timestamp: first, group: 'default', ttft_ms: 100 },
      { timestamp: second, group: 'default', ttft_ms: 120 },
    ])
    const uptime = buildUptimeChartData([
      { date: first, uptime_pct: 99, incidents: 1, outage_minutes: 0 },
      { date: second, uptime_pct: 100, incidents: 0, outage_minutes: 0 },
    ])

    assert.notEqual(latency[0].time, latency[1].time)
    assert.notEqual(uptime[0].date, uptime[1].date)
  })

  test('downsamples dense uptime series while preserving the worst point in each window', () => {
    const series = Array.from({ length: 100 }, (_, index) => ({
      date: new Date((index + 1) * 60_000).toISOString(),
      uptime_pct: index === 49 ? 12.5 : 100,
      incidents: index === 49 ? 1 : 0,
      outage_minutes: 0,
    }))

    const sampled = downsampleUptimeSeries(series, 32)

    assert.equal(sampled.length, 32)
    assert.ok(sampled.some((point) => point.uptime_pct === 12.5))
  })

  test('uses the weighted overall uptime supplied by the backend', async () => {
    const view = await testEnv.render(
      <UptimeSparkline
        series={[
          {
            date: new Date(60_000).toISOString(),
            uptime_pct: 0,
            incidents: 1,
            outage_minutes: 0,
          },
          {
            date: new Date(120_000).toISOString(),
            uptime_pct: 100,
            incidents: 0,
            outage_minutes: 0,
          },
        ]}
        overallPct={99}
      />
    )

    try {
      assert.ok(view.container.textContent?.includes('99.0%'))
      assert.ok(!view.container.textContent?.includes('50.0%'))
    } finally {
      await view.unmount()
    }
  })
})
