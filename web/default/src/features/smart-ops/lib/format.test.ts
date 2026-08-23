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
import { formatLegacyLatency } from './format'

describe('formatLegacyLatency', () => {
  test('keeps a recorded zero-second average distinct from missing data', () => {
    assert.equal(formatLegacyLatency(0), '0ms')
    assert.equal(formatLegacyLatency(null), '—')
  })

  test('uses the shared latency format for positive values', () => {
    assert.equal(formatLegacyLatency(500), '500ms')
    assert.equal(formatLegacyLatency(1_500), '1.50s')
  })
})
