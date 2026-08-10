import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS,
  getAdvancedCustomIncomingPathLabel,
} from './advanced-custom'

describe('advanced custom alpha search route', () => {
  test('offers the standalone alpha search endpoint', () => {
    assert.ok(
      ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS.some(
        (option) =>
          option.value === '/v1/alpha/search' &&
          option.label === 'OpenAI Alpha Search'
      )
    )
    assert.equal(
      getAdvancedCustomIncomingPathLabel('/v1/alpha/search'),
      'OpenAI Alpha Search'
    )
  })
})
