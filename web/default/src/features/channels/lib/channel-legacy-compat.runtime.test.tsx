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
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { test } from 'node:test'
import { pathToFileURL } from 'node:url'
import { api } from '@/lib/api'
import { getChannel, getChannels, searchChannels } from '../api'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToUpdatePayload,
} from './channel-form'
import { readChannelRoutingHealth } from './channel-routing-health'

type Envelope = { success: boolean; data: unknown }
type Capture = {
  id: number
  list: Envelope
  search: Envelope
  detail: Envelope
  saved: Envelope
}
const fixtures = process.env.MAXAPI_P44_FIXTURES

test(
  'P44 pinned historical schema and CRUD consume both real controller versions and emit a configuration-only PUT',
  {
    skip:
      !fixtures &&
      'Run the P4.4 isolated recipe to supply historical sources and real controller captures',
  },
  async () => {
    assert.ok(fixtures)
    const read = (name: string) =>
      JSON.parse(readFileSync(join(fixtures, name), 'utf8'))
    const provenance = read('history.json') as {
      revision: string
      modules: {
        file: string
        original_file: string
        source_blob: string
        materialized_sha256: string
      }[]
      unchanged: { path: string; historical: string; current: string }[]
    }
    assert.equal(
      provenance.revision,
      'b2745bbaa9113a11dbe55c03eb4faf0ee213195e'
    )
    const originalBlobs: Record<string, string> = {
      'history/channel.go': '386b27ac9e442c65ab5994767c1fd66e413196ab',
      'history/types.ts': '62ece3eea312a2f116aa8588097f034b568dfdac',
      'history/api.ts': '7ea7872eda76b5933615b168e495a28c1835fa32',
    }
    for (const module of provenance.modules) {
      const original: Uint8Array = readFileSync(
        join(fixtures, module.original_file)
      )
      assert.equal(
        createHash('sha1')
          .update(`blob ${original.length}\0`)
          .update(original)
          .digest('hex'),
        originalBlobs[module.file]
      )
      assert.equal(module.source_blob, originalBlobs[module.file])
      assert.equal(
        createHash('sha256')
          .update(readFileSync(join(fixtures, module.file)))
          .digest('hex'),
        module.materialized_sha256
      )
    }
    for (const dependency of provenance.unchanged) {
      assert.equal(dependency.current, dependency.historical, dependency.path)
    }
    // Only import specifiers are relocated by the recipe. Runtime schema and
    // CRUD bodies come from the pinned Git objects, not from current functions.
    const legacyTypes = (await import(
      pathToFileURL(join(fixtures, 'history/types.ts')).href
    )) as Pick<typeof import('../types'), 'channelSchema'>
    const legacyApi = (await import(
      pathToFileURL(join(fixtures, 'history/api.ts')).href
    )) as Pick<
      typeof import('../api'),
      'getChannels' | 'searchChannels' | 'getChannel' | 'updateChannel'
    >
    const current = read('current.json') as Capture
    const legacy = read('legacy.json') as Capture
    assert.equal(current.id, legacy.id)
    // The historical editor passes response.data directly to the converter
    // (b2745bba drawer:772); it does not schema.parse the backend's nullable
    // configuration fields. Exercise the schema separately with a valid input.
    const schemaInput = {
      id: current.id,
      type: 1,
      key: '',
      status: 1,
      name: 'synthetic-p44',
      created_time: 0,
      test_time: 0,
      response_time: 0,
      balance_updated_time: 0,
      priority: 10,
      routing_health: (current.detail.data as Record<string, unknown>)
        .routing_health,
    }
    const schemaParsed = legacyTypes.channelSchema.parse(schemaInput)
    assert.equal(schemaParsed.priority, 10)
    assert.equal(Object.hasOwn(schemaParsed, 'routing_health'), false)
    // Compatibility means the entire existing envelope remains identical after
    // removing exactly the new read-only field, not just that priority matches.
    for (const name of ['list', 'search', 'detail', 'saved'] as const) {
      const stripped = structuredClone(current[name])
      const data = stripped.data as Record<string, unknown>
      const items = Array.isArray(data.items) ? data.items : [data]
      for (const item of items)
        delete (item as Record<string, unknown>).routing_health
      assert.deepEqual(stripped, legacy[name], name)
    }
    let selected = current
    const writes: Record<string, unknown>[] = []
    const adapter = api.defaults.adapter
    api.defaults.adapter = async (config) => {
      let data: Envelope
      if (config.method === 'put') {
        writes.push(JSON.parse(String(config.data)))
        data = { success: true, data: selected.saved.data }
      } else if (config.url === '/api/channel/search') data = selected.search
      else if (config.url === '/api/channel') data = selected.list
      else {
        assert.equal(config.url, `/api/channel/${selected.id}`)
        data = selected.detail
      }
      return { config, status: 200, statusText: 'OK', headers: {}, data }
    }
    try {
      for (const capture of [current, legacy]) {
        selected = capture
        assert.deepEqual(
          await legacyApi.getChannels({ page_size: 20 }),
          capture.list
        )
        assert.deepEqual(
          await legacyApi.searchChannels({ keyword: 'origin' }),
          capture.search
        )
        const oldDetail = await legacyApi.getChannel(capture.id)
        assert.deepEqual(oldDetail, capture.detail)
        assert.ok(oldDetail.data)
        assert.equal(oldDetail.data.priority, 10)
        // This converter and its dependencies are byte-identical Git blobs in
        // the historical revision and HEAD; the recipe verifies that premise.
        const defaults = transformChannelToFormDefaults(oldDetail.data)
        const payload = transformFormDataToUpdatePayload(
          { ...CHANNEL_FORM_DEFAULT_VALUES, ...defaults, priority: 20 },
          capture.id
        )
        await legacyApi.updateChannel(capture.id, payload)
        assert.equal(writes.at(-1)!.priority, 20)
        for (const field of [
          'routing_health',
          'effective_priority',
          'observation',
        ]) {
          assert.equal(Object.hasOwn(writes.at(-1)!, field), false)
        }
        // New API readers can also read every unchanged legacy envelope.
        assert.equal((await getChannels({ page_size: 20 })).success, true)
        assert.equal(
          (await searchChannels({ keyword: 'origin' })).success,
          true
        )
        const response = await getChannel(capture.id)
        assert.ok(response.data)
        const observation = readChannelRoutingHealth(
          response.data.routing_health
        )
        assert.equal(
          observation.kind,
          capture === current ? 'known' : 'missing'
        )
        if (observation.kind === 'known') {
          assert.equal(observation.health.effective_priority, 8)
        }
      }
      assert.equal(writes.length, 2)
      assert.deepEqual(writes[0], writes[1])
      // Both real Go controller versions consume this exact serialized PUT in
      // the next recipe steps; this is not a mocked persistence assertion.
      writeFileSync(
        join(fixtures, 'legacy-save.json'),
        JSON.stringify(writes[0])
      )
    } finally {
      api.defaults.adapter = adapter
    }
  }
)
