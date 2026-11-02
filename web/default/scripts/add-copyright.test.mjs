// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { test } from 'node:test'
import { fileURLToPath } from 'node:url'

const script = fileURLToPath(new URL('./add-copyright.mjs', import.meta.url))

test('copyright normalization preserves mixed upstream notices when normalizing files', (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'maxapi-copyright-'))
  t.after(() => {
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()))
    assert.ok(path.basename(root).startsWith('maxapi-copyright-'))
    fs.rmSync(root, { recursive: true, force: true })
  })
  fs.mkdirSync(path.join(root, 'src'))
  const original = `/*
Copyright (C) 2023-2026 MAX-API-Next
Copyright (C) 2023-2026 QuantumNous

Licensed under AGPL-3.0-or-later.
For commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues
*/
export const value = 1
`
  const file = path.join(root, 'src', 'adapted.ts')
  fs.writeFileSync(file, original)
  // Exercise the destructive mode directly; a valid mixed notice stays byte-identical.
  execFileSync(process.execPath, [script], { cwd: root })
  assert.equal(fs.readFileSync(file, 'utf8'), original)
})
