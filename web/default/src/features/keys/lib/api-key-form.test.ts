/*
Copyright (C) 2023-2026 MAX-API-Next

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import type { TFunction } from 'i18next'
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { apiKeySchema, type ApiKey } from '../types'
import {
  getApiKeyFormSchema,
  getApiKeyFormDefaultValues,
  shouldIncludeRoutingProjection,
  transformApiKeyToFormDefaults,
  transformFormDataToPayload,
} from './api-key-form'

const translate = ((key: string) => key) as TFunction

describe('API key routing form', () => {
  test('defaults new keys to the configured automatic routing group', () => {
    const values = getApiKeyFormDefaultValues('auto', ['base', 'default'])
    const payload = transformFormDataToPayload(values)

    assert.equal(values.routing_mode, 'smart')
    assert.deepEqual(values.manual_groups, ['base', 'default'])
    assert.deepEqual(payload.routing, {
      version: 1,
      mode: 'smart',
      route: 'auto',
      retry_on_failure: true,
    })
  })

  test('preserves ordered manual groups in the payload', () => {
    const values = {
      ...getApiKeyFormDefaultValues(),
      routing_mode: 'manual' as const,
      manual_groups: ['vip', 'default', 'base'],
    }
    const payload = transformFormDataToPayload(values)

    assert.equal(payload.group, 'vip')
    assert.deepEqual(payload.routing?.groups, ['vip', 'default', 'base'])
  })

  test('maps legacy real-group keys to manual routing', () => {
    const apiKey = {
      id: 1,
      name: 'legacy',
      key: 'sk-***',
      status: 1,
      remain_quota: 0,
      used_quota: 0,
      unlimited_quota: true,
      expired_time: -1,
      created_time: 1,
      accessed_time: 1,
      group: 'vip',
      cross_group_retry: false,
      model_limits_enabled: false,
      model_limits: '',
      allow_ips: '',
    } satisfies ApiKey

    const values = transformApiKeyToFormDefaults(apiKey)
    assert.equal(values.routing_mode, 'manual')
    assert.deepEqual(values.manual_groups, ['vip'])
  })

  test('rejects unavailable smart and manual routing selections', () => {
    const schema = getApiKeyFormSchema(translate, {
      smartRoutes: ['auto'],
      manualGroups: ['default', 'vip'],
    })

    const smart = schema.safeParse({
      ...getApiKeyFormDefaultValues('auto:removed'),
      routing_route: 'auto:removed',
    })
    assert.equal(smart.success, false)

    const manual = schema.safeParse({
      ...getApiKeyFormDefaultValues(),
      routing_mode: 'manual',
      manual_groups: ['vip', 'removed'],
    })
    assert.equal(manual.success, false)
  })

  test('preserves an unavailable current smart route when routing is unchanged', () => {
    const schema = getApiKeyFormSchema(translate, {
      smartRoutes: ['auto'],
      manualGroups: ['default'],
      preservedSmartRoute: 'auto:internal',
    })

    const apiKey = {
      id: 4,
      name: 'hidden-route-key',
      key: 'sk-***',
      status: 1,
      remain_quota: 0,
      used_quota: 0,
      unlimited_quota: true,
      expired_time: -1,
      created_time: 1,
      accessed_time: 1,
      group: 'auto:internal',
      cross_group_retry: true,
      routing: {
        version: 1,
        mode: 'smart',
        route: 'auto:internal',
        retry_on_failure: true,
      },
      model_limits_enabled: false,
      model_limits: '',
      allow_ips: '',
    } satisfies ApiKey
    const values = {
      ...transformApiKeyToFormDefaults(apiKey),
      name: 'preserved-route-key',
    }

    assert.equal(schema.safeParse(values).success, true)
    assert.equal(shouldIncludeRoutingProjection(true, true, false), false)
  })

  test('uses the selected smart route groups when switching to manual mode', () => {
    const apiKey = {
      id: 2,
      name: 'smart-key',
      key: 'sk-***',
      status: 1,
      remain_quota: 0,
      used_quota: 0,
      unlimited_quota: true,
      expired_time: -1,
      created_time: 1,
      accessed_time: 1,
      group: 'auto:fast',
      cross_group_retry: true,
      routing: {
        version: 1,
        mode: 'smart',
        route: 'auto:fast',
        retry_on_failure: false,
      },
      model_limits_enabled: false,
      model_limits: '',
      allow_ips: '',
    } satisfies ApiKey

    const values = transformApiKeyToFormDefaults(apiKey, ['fast', 'default'])
    assert.deepEqual(values.manual_groups, ['fast', 'default'])
    assert.equal(values.cross_group_retry, false)
  })

  test('omits routing fields when an unchanged legacy key is updated', () => {
    assert.equal(shouldIncludeRoutingProjection(true, true, false), false)
    assert.equal(shouldIncludeRoutingProjection(true, true, true), true)
    assert.equal(shouldIncludeRoutingProjection(true, false, false), true)

    const payload = transformFormDataToPayload(
      getApiKeyFormDefaultValues('auto', ['default']),
      { includeRouting: false }
    )

    assert.equal('routing' in payload, false)
    assert.equal('group' in payload, false)
    assert.equal('cross_group_retry' in payload, false)
  })

  test('ignores removed strategy fields in older routing responses', () => {
    const apiKey = apiKeySchema.parse({
      id: 3,
      name: 'old-policy',
      key: 'sk-***',
      status: 1,
      remain_quota: 0,
      used_quota: 0,
      unlimited_quota: true,
      expired_time: -1,
      created_time: 1,
      accessed_time: 1,
      group: 'auto',
      cross_group_retry: true,
      routing: {
        version: 1,
        mode: 'smart',
        route: 'auto',
        strategy: 'price',
        allow_request_override: true,
        retry_on_failure: true,
      },
      model_limits_enabled: false,
      model_limits: '',
      allow_ips: '',
    })

    assert.deepEqual(apiKey.routing, {
      version: 1,
      mode: 'smart',
      route: 'auto',
      retry_on_failure: true,
    })
  })
})
