import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  PLAN_FORM_DEFAULTS,
  formValuesToPlanPayload,
  planToFormValues,
} from './plan-form'

describe('subscription plan policy fields', () => {
  test('defaults wallet overflow to enabled and keeps explicit false', () => {
    assert.equal(PLAN_FORM_DEFAULTS.allow_wallet_overflow, true)

    const values = planToFormValues({
      id: 1,
      title: 'Strict',
      subtitle: '',
      price_amount: 1,
      currency: 'USD',
      duration_unit: 'month',
      duration_value: 1,
      custom_seconds: 0,
      quota_reset_period: 'never',
      quota_reset_custom_seconds: 0,
      enabled: true,
      sort_order: 0,
      allow_balance_pay: true,
      allow_wallet_overflow: false,
      max_purchase_per_user: 0,
      total_amount: 100,
      upgrade_group: 'svip',
      downgrade_group: 'default',
    })
    assert.equal(values.allow_wallet_overflow, false)
    assert.equal(values.downgrade_group, 'default')

    const payload = formValuesToPlanPayload(values)
    assert.equal(payload.plan.allow_wallet_overflow, false)
    assert.equal(payload.plan.downgrade_group, 'default')
  })
})
