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
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { test } from 'node:test'
import {
  applyTestShard,
  buildTestCommand,
  discoverTestFiles,
  extractShardArgument,
  selectTestFiles,
  splitTestArguments,
  usesIsolatedEnvironment,
} from './run-tests.mjs'

test('discovers every supported test extension outside only generated directories', async () => {
  const root = await mkdtemp(path.join(tmpdir(), 'max-api-run-tests-'))
  try {
    const files = [
      'scripts/runner.test.mjs',
      'scripts/legacy_test.cts',
      'src/component.spec.jsx',
      'src/legacy_spec.mts',
      'tests/example.test.js',
      'node_modules/dependency.test.js',
      'dist/generated.spec.mjs',
    ]
    for (const file of files) {
      const absolutePath = path.join(root, file)
      await mkdir(path.dirname(absolutePath), { recursive: true })
      await writeFile(absolutePath, '')
    }

    assert.deepEqual(await discoverTestFiles(root), [
      'scripts/legacy_test.cts',
      'scripts/runner.test.mjs',
      'src/component.spec.jsx',
      'src/legacy_spec.mts',
      'tests/example.test.js',
    ])
  } finally {
    await rm(root, { force: true, recursive: true })
  }
})

test('separates Bun flags from file filters without dropping flag values', () => {
  assert.deepEqual(
    splitTestArguments([
      '--test-name-pattern',
      'renders details',
      '--coverage',
      '--parallel',
      '1',
      '--preload',
      './test/setup.ts',
      '--env-file',
      '.env.test',
      'src/features/example.test.ts',
    ]),
    {
      bunArguments: [
        '--test-name-pattern',
        'renders details',
        '--coverage',
        '--parallel',
        '1',
        '--preload',
        './test/setup.ts',
        '--env-file',
        '.env.test',
      ],
      fileFilters: ['src/features/example.test.ts'],
    }
  )

  assert.deepEqual(splitTestArguments(['--bail', 'src/example.test.ts']), {
    bunArguments: ['--bail'],
    fileFilters: ['src/example.test.ts'],
  })
  assert.deepEqual(
    splitTestArguments(['--shard', '2/3', 'src/example.test.ts']),
    {
      bunArguments: ['--shard', '2/3'],
      fileFilters: ['src/example.test.ts'],
    }
  )
  assert.deepEqual(splitTestArguments(['--changed', 'origin/main']), {
    bunArguments: ['--changed', 'origin/main'],
    fileFilters: [],
  })
  assert.deepEqual(
    splitTestArguments([
      '--conditions',
      'development',
      '--define',
      'MAX_API_TEST_VALUE=1',
      '--loader',
      '.txt:text',
      '--tsconfig-override',
      './tsconfig.test.json',
      'src/example.test.ts',
    ]),
    {
      bunArguments: [
        '--conditions',
        'development',
        '--define',
        'MAX_API_TEST_VALUE=1',
        '--loader',
        '.txt:text',
        '--tsconfig-override',
        './tsconfig.test.json',
      ],
      fileFilters: ['src/example.test.ts'],
    }
  )
})

test('applies plain and glob file filters to the discovered test list', () => {
  const files = [
    'scripts/run-tests.test.mjs',
    'src/features/a/alpha.test.ts',
    'src/features/b/beta.spec.tsx',
  ]

  assert.deepEqual(selectTestFiles(files, ['alpha']), [
    'src/features/a/alpha.test.ts',
  ])
  assert.deepEqual(selectTestFiles(files, ['scripts/*.test.mjs']), [
    'scripts/run-tests.test.mjs',
  ])
})

test('applies Bun shards across the complete selected file list', () => {
  const parsed = extractShardArgument(['--coverage', '--shard=2/3'])
  assert.deepEqual(parsed, {
    bunArguments: ['--coverage'],
    shard: { index: 2, total: 3 },
  })
  assert.deepEqual(
    applyTestShard(['a.test.ts', 'b.test.ts', 'c.test.ts', 'd.test.ts'], parsed.shard),
    ['b.test.ts']
  )
})

test('keeps the child test process serial when callers set concurrency', () => {
  const files = ['src/example.test.ts']
  const command = buildTestCommand(files, [
    '--coverage',
    '--max-concurrency',
    '8',
    '--timeout',
    '5000',
    '--max-concurrency=4',
  ])

  assert.deepEqual(command, [
    process.execPath,
    'test',
    '--coverage',
    '--timeout',
    '5000',
    '--max-concurrency=1',
    ...files,
  ])
  assert.equal(
    command.filter((argument) => argument.startsWith('--max-concurrency')).length,
    1
  )
  assert.ok(command.indexOf('--max-concurrency=1') < command.indexOf(files[0]))
})

test('rejects malformed caller concurrency values', () => {
  const files = ['src/example.test.ts']

  assert.throws(
    () => buildTestCommand(files, ['--max-concurrency']),
    /--max-concurrency requires a numeric value/
  )
  assert.throws(
    () => buildTestCommand(files, ['--max-concurrency', 'many']),
    /Invalid --max-concurrency value: many/
  )
  assert.throws(
    () => buildTestCommand(files, ['--max-concurrency=']),
    /Invalid --max-concurrency value:/
  )
  assert.throws(
    () => buildTestCommand(files, ['--max-concurrency=many']),
    /Invalid --max-concurrency value: many/
  )
})

test('isolates tests that mutate browser globals even when they are plain TypeScript', () => {
  assert.equal(
    usesIsolatedEnvironment(
      'src/example.test.ts',
      "Object.defineProperty(\n  globalThis,\n  'window',\n  { value: {} }\n)"
    ),
    true
  )
  assert.equal(
    usesIsolatedEnvironment('src/example.test.ts', 'delete globalThis.window'),
    true
  )
  assert.equal(
    usesIsolatedEnvironment(
      'src/example.test.ts',
      "globalThis['window'] = {}"
    ),
    true
  )
  assert.equal(
    usesIsolatedEnvironment('src/example.test.ts', 'globalThis[propertyName] = {}'),
    true
  )
  assert.equal(
    usesIsolatedEnvironment(
      'src/example.test.ts',
      "globalThis['window'] === existingWindow"
    ),
    false
  )
  assert.equal(
    usesIsolatedEnvironment('src/example.test.ts', 'assert.equal(1, 1)'),
    false
  )
})
