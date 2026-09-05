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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildH3BillingFormValues,
  buildH3BillingProfile,
  DEFAULT_H3_BILLING_PROFILE,
  H3_BILLING_PROFILE_KEY,
  hasLegacyH3RateCard,
  parseH3BillingProfiles,
  serializeH3BillingProfiles,
} from './h3-billing-utils'

describe('H3 billing configuration utilities', () => {
  test('parses the profile and preserves other structured profiles', () => {
    const otherProfile = {
      ...DEFAULT_H3_BILLING_PROFILE,
      currency: 'CNY',
    }
    const raw = JSON.stringify({
      [H3_BILLING_PROFILE_KEY]: DEFAULT_H3_BILLING_PROFILE,
      other_profile: otherProfile,
    })

    const result = parseH3BillingProfiles(raw)

    assert.equal(result.error, undefined)
    assert.equal(result.profiles.other_profile.currency, 'CNY')
    assert.deepEqual(
      buildH3BillingFormValues(result.profiles[H3_BILLING_PROFILE_KEY]),
      {
        output768Price: '0.08',
        output2KPrice: '0.13',
        inputVideo768Price: '0.08',
        inputVideo2KPrice: '0.13',
        inputVideoMaxSeconds: 15,
        inputImageFreeCount: 5,
        inputImageExtraPrice: '0.04',
      }
    )
  })

  test('rejects missing profile instead of allowing an empty save', () => {
    const result = parseH3BillingProfiles('{}')

    assert.match(result.error || '', /minimax_h3_v2 is required/)
  })

  test('builds an editable profile without changing immutable audio pricing', () => {
    const profile = buildH3BillingProfile(DEFAULT_H3_BILLING_PROFILE, {
      output768Price: '0.10',
      output2KPrice: '0.20',
      inputVideo768Price: '0.11',
      inputVideo2KPrice: '0.21',
      inputVideoMaxSeconds: 10,
      inputImageFreeCount: 2,
      inputImageExtraPrice: '0.05',
    })

    assert.equal(profile.output_unit_price['768P'], '0.10')
    assert.equal(profile.input_video_max_seconds, 10)
    assert.equal(profile.input_audio_unit_price, '0')
    assert.equal(
      serializeH3BillingProfiles({
        [H3_BILLING_PROFILE_KEY]: profile,
      }).includes('0.10'),
      true
    )
  })

  test('detects only the legacy MiniMax-H3 rate-card key', () => {
    assert.equal(hasLegacyH3RateCard('{"MiniMax-H3":{"unit":"second"}}'), true)
    assert.equal(hasLegacyH3RateCard('{"minimax_h3_v2":{}}'), false)
    assert.equal(hasLegacyH3RateCard('{invalid'), false)
  })
})
