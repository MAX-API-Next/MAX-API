// Copyright (C) 2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { test } from 'node:test'
import { readPluginFile } from './file-reader'

for (const text of [
  '\uFEFFexport const 名称 = "测试";\r\n',
  '\uFEFF{"source":"测试"}\r\n',
  '{"source":"测试"}\r\n',
]) {
  test(`plugin file preserves UTF-8 text bytes (BOM=${text.startsWith('\uFEFF')})`, async () => {
    const bytes = new TextEncoder().encode(text)
    const file = {
      text: async () => new TextDecoder().decode(bytes),
      arrayBuffer: async () => Uint8Array.from(bytes).buffer,
    }
    const actual = await readPluginFile(file)
    assert.equal(actual, text)
    assert.equal(
      createHash('sha256').update(actual).digest('hex'),
      createHash('sha256').update(bytes).digest('hex')
    )
  })
}
