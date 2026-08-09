import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { buildResponsesCompactSample } from './model-details-api-samples'

describe('model details API samples', () => {
  for (const language of [
    'curl',
    'python',
    'typescript',
    'javascript',
  ] as const) {
    test(`builds a Responses Compact request for ${language}`, () => {
      const sample = buildResponsesCompactSample(language, {
        baseUrl: 'https://api.example.com',
        apiKeyEnv: 'MAX_API_KEY',
        modelName: 'gpt-5.1-codex-mini',
        endpointPath: '/v1/responses/compact',
      })

      assert.match(sample, /\/v1\/responses\/compact/)
      assert.match(sample, /input/)
      assert.doesNotMatch(sample, /messages/)
      assert.doesNotMatch(sample, /temperature/)
    })
  }
})
