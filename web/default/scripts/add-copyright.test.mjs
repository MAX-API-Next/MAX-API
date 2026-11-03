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
// Copyright (C) 2023-2026 MAX-API-Next. Licensed under AGPL-3.0-or-later.
import assert from 'node:assert/strict'
import { execFileSync, spawnSync } from 'node:child_process'
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

function copyrightFixture(t, original) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'maxapi-copyright-'))
  t.after(() => {
    assert.equal(path.dirname(root), path.resolve(os.tmpdir()))
    assert.ok(path.basename(root).startsWith('maxapi-copyright-'))
    fs.rmSync(root, { recursive: true, force: true })
  })
  fs.mkdirSync(path.join(root, 'src'))
  const file = path.join(root, 'src', 'adversarial.ts')
  fs.writeFileSync(file, original)
  return { root, file }
}

function preserveNotice(t, original, verify = (actual) => assert.equal(actual, original)) {
  const { root, file } = copyrightFixture(t, original)
  execFileSync(process.execPath, [script], { cwd: root })
  verify(fs.readFileSync(file, 'utf8'))
}

test('copyright check detects an outdated project header before a sole project notice', (t) => {
  const suffix = '// Copyright (C) 2023-2026 MAX-API-Next\nexport const value = 1\n'
  const original = '/*\nCopyright (C) 2020 MAX-API-Next\n\nFor commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues\n*/\n' + suffix
  const { root, file } = copyrightFixture(t, original)
  const before = spawnSync(process.execPath, [script, '--check'], { cwd: root })
  assert.equal(before.status, 1)
  assert.equal(fs.readFileSync(file, 'utf8'), original)
  execFileSync(process.execPath, [script], { cwd: root })
  const updated = fs.readFileSync(file, 'utf8')
  assert.ok(updated.startsWith('/*\nCopyright (C) 2023-2026 MAX-API-Next\n'))
  assert.ok(updated.endsWith('*/\n' + suffix))
  assert.equal(spawnSync(process.execPath, [script, '--check'], { cwd: root }).status, 0)
  assert.equal(fs.readFileSync(file, 'utf8'), updated)
})

test('copyright preserves a mixed later notice after a recognized project block', (t) => {
  preserveNotice(t, '/*\nCopyright (C) 2020 MAX-API-Next\n\nFor commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues\n*/\n// Copyright (C) 2023 MAX-API-Next and QuantumNous\nexport const value = 1\n')
})

test('copyright does not normalize a standalone project notice without a recognized block', (t) => {
  preserveNotice(t, '// Copyright (C) 2020 MAX-API-Next\nexport const value = 1\n')
})

test('copyright preserves a standalone license declaration', (t) => {
  preserveNotice(t, '/* Licensed under MIT */\nexport const value = 1\n')
})

test('copyright preserves machine-readable project SPDX metadata inside its block', (t) => {
  preserveNotice(t, '/*\nCopyright (C) 2020 MAX-API-Next\nSPDX-FileCopyrightText: 2020 MAX-API-Next\n\nFor commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues\n*/\nexport const value = 1\n')
})
test('copyright keeps a star-prefixed upstream holder inside a project block', (t) => {
  preserveNotice(t, '/*\nCopyright (C) 2023-2026 MAX-API-Next\n * Copyright (C) 2023-2026 QuantumNous\n\nFor commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues\n*/\nexport const value = 1\n')
})
test('copyright matching cannot cross an incomplete header into later upstream blocks', (t) => {
  const canonical = fs.readFileSync(script, 'utf8').match(/^\/\*[\s\S]*?\*\//)[0]
  preserveNotice(t, '/*\nCopyright (C) 2023-2026 MAX-API-Next\nIncomplete project notice\n*/\n/*\nCopyright (C) 2026 QuantumNous\nKeep this upstream notice\n*/\n' + canonical + '\nexport const value = 1\n')
})

for (const notice of [
  'Copyright (C) 2023 QuantumNous and MAX-API-Next',
  'Copyright (C) 2023 MAX-API-Next and QuantumNous',
  'SPDX-FileCopyrightText: 2023 QuantumNous',
  '* SPDX-FileCopyrightText: 2023 QuantumNous and MAX-API-Next',
  '© 2024 QuantumNous',
  '* © QuantumNous (legacy attribution)',
  'Authored by QuantumNous',
]) {
  test('copyright preserves attribution: ' + notice, (t) => {
    preserveNotice(t, '/*\nCopyright (C) 2023-2026 MAX-API-Next\n' + notice + '\n\nFor commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues\n*/\nexport const value = 1\n')
  })
}

test('copyright preserves a standalone symbol notice', (t) => {
  preserveNotice(t, '/*\n© 2024 QuantumNous\n*/\nexport const value = 1\n')
})

for (const header of [
  '/* Module description. */\n/* Copyright (C) 2023 Upstream */',
  '/*! Copyright (C) 2023 Upstream */',
  '// Module description.\n/* SPDX-FileCopyrightText: 2023 Upstream */',
  '\n // Copyright (C) 2023 Upstream',
  '/*! Vendor license text without a standard copyright spelling. */',
  '/**! Copyright (C) 2023 Upstream */',
  '//! Copyright (C) 2023 Upstream',
  '/// Copyright (C) 2023 Upstream',
  '/* SPDX-License-Identifier: MIT */',
  '/** @license MIT */',
  '/* Portions Copyright (C) 2023 QuantumNous */',
  '/* Based on work: Copyright 2023 QuantumNous */',
]) {
  test('copyright preserves leading notice sequence: ' + header, (t) => {
    preserveNotice(t, header + '\nexport const value = 1\n')
  })
}

test('copyright normalizes a project-only notice with a trailing period', (t) => {
  preserveNotice(t, '/*\nCopyright (C) 2020 MAX-API-Next.\n\nFor commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues\n*/\nexport const value = 1\n', (actual) => {
    assert.ok(actual.startsWith('/*\nCopyright (C) 2023-2026 MAX-API-Next\n'))
    assert.ok(!actual.includes('2020 MAX-API-Next.'))
    assert.ok(actual.endsWith('export const value = 1\n'))
  })
})

test('copyright adds its header before ordinary prose mentioning the symbol', (t) => {
  const original = '/* Render the © symbol in the footer. */\nexport const value = 1\n'
  preserveNotice(t, original, (actual) => {
    assert.ok(actual.startsWith('/*\nCopyright (C) 2023-2026 MAX-API-Next\n'))
    assert.ok(actual.endsWith(original))
  })
})

test('copyright adds its header before prose describing the copyright label', (t) => {
  const original = '/* Render the Copyright label in the footer. */\nexport const value = 1\n'
  preserveNotice(t, original, (actual) => {
    assert.ok(actual.startsWith('/*\nCopyright (C) 2023-2026 MAX-API-Next\n'))
    assert.ok(actual.endsWith(original))
  })
})

test('copyright still normalizes a recognized sole project holder', (t) => {
  preserveNotice(t, '/*\nCopyright (C) 2020-2021 MAX-API-Next\n\nFor commercial licensing, please contact https://github.com/MAX-API-Next/MAX-API/issues\n*/\nexport const value = 1\n', (actual) => {
    assert.ok(actual.startsWith('/*\nCopyright (C) 2023-2026 MAX-API-Next\n'))
    assert.ok(actual.endsWith('export const value = 1\n'))
    assert.ok(!actual.includes('2020-2021'))
  })
})
