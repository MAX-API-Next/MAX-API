import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildAlphaSearchSample,
  buildResponsesCompactSample,
} from './model-details-api-samples'

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

  for (const language of [
    'curl',
    'python',
    'typescript',
    'javascript',
  ] as const) {
    test(`builds an Alpha Search request for ${language}`, () => {
      const sample = buildAlphaSearchSample(language, {
        baseUrl: 'https://api.example.com',
        apiKeyEnv: 'MAX_API_KEY',
        modelName: 'gpt-5.1-codex-mini',
        endpointPath: '/v1/alpha/search',
      })

      assert.match(sample, /\/v1\/alpha\/search/)
      assert.match(sample, /"search_query"\s*:\s*\[/)
      assert.match(sample, /"q"\s*:\s*"latest artificial intelligence news"/)
      assert.match(sample, /gpt-5\.1-codex-mini/)

      if (language === 'curl') {
        assert.match(sample, /Authorization: Bearer \$MAX_API_KEY/)
      } else if (language === 'python') {
        assert.match(sample, /os\.environ\['MAX_API_KEY'\]/)
      } else {
        assert.match(sample, /process\.env\.MAX_API_KEY/)
      }
    })
  }
})
