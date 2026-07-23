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
import { getModelMappingGuardrail } from './channel-editor-state'

describe('model mapping guardrail', () => {
  test('reports missing source models and exposed upstream targets once', () => {
    const guardrail = getModelMappingGuardrail(
      JSON.stringify({
        'client-model': 'vendor-model',
        'second-client-model': 'vendor-model',
        incomplete: '',
      }),
      ['client-model', 'vendor-model']
    )

    assert.equal(guardrail.invalidJson, false)
    assert.deepEqual(guardrail.entries, [
      { source: 'client-model', target: 'vendor-model' },
      { source: 'second-client-model', target: 'vendor-model' },
    ])
    assert.deepEqual(guardrail.missingSourceModels, ['second-client-model'])
    assert.deepEqual(guardrail.exposedTargetModels, ['vendor-model'])
  })

  test('rejects invalid JSON and non-object mappings', () => {
    assert.equal(getModelMappingGuardrail('{', []).invalidJson, true)
    assert.equal(getModelMappingGuardrail('[]', []).invalidJson, true)
    assert.equal(getModelMappingGuardrail('', []).invalidJson, false)
  })

  test('rejects mappings with non-string targets', () => {
    const guardrail = getModelMappingGuardrail(
      JSON.stringify({
        valid: 'upstream-model',
        number: 123,
        object: { model: 'upstream-model' },
      }),
      ['valid', 'upstream-model']
    )

    assert.equal(guardrail.invalidJson, true)
    assert.deepEqual(guardrail.entries, [])
    assert.deepEqual(guardrail.missingSourceModels, [])
    assert.deepEqual(guardrail.exposedTargetModels, [])
  })
})
