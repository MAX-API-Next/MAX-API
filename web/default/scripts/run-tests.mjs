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
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url))
const projectRoot = path.resolve(scriptDirectory, '..')
const testGlob = new Bun.Glob(
  '**/*{.test,_test,.spec,_spec}.{ts,tsx,js,jsx,mjs,cjs,mts,cts}'
)
const ignoredDirectories = new Set([
  '.git',
  'coverage',
  'dist',
  'node_modules',
])
const flagsWithSeparateValues = new Set([
  '-t',
  '--conditions',
  '--coverage-dir',
  '--coverage-reporter',
  '--define',
  '--env-file',
  '--loader',
  '--max-concurrency',
  '--parallel-delay',
  '--path-ignore-patterns',
  '--preload',
  '--reporter',
  '--reporter-outfile',
  '--rerun-each',
  '--retry',
  '--seed',
  '--test-name-pattern',
  '--timeout',
  '--tsconfig-override',
])
const optionalValueFlags = new Map([
  ['--bail', (value) => /^\d+$/.test(value)],
  ['--changed', () => true],
  ['--parallel', (value) => /^\d+$/.test(value)],
])
const textDecoder = new TextDecoder()
const shardValuePattern = /^(\d+)\/(\d+)$/
const maxConcurrencyValuePattern = /^\d+$/
const globalMutationPattern =
  /(?:Object\.defineProperty|Reflect\.deleteProperty)\s*\(\s*globalThis\b|delete\b\s*(?:\(\s*)?globalThis\b|globalThis(?:\.[A-Za-z_$][\w$]*|\s*\[[^\]]*\])\s*=(?!=)/

function normalizePath(file) {
  return file.replaceAll('\\', '/')
}

function isIgnoredTestPath(file) {
  return normalizePath(file)
    .split('/')
    .some((segment) => ignoredDirectories.has(segment))
}

export async function discoverTestFiles(root = projectRoot) {
  const testFiles = []
  for await (const file of testGlob.scan({ cwd: root, onlyFiles: true })) {
    const normalizedFile = normalizePath(file)
    if (!isIgnoredTestPath(normalizedFile)) testFiles.push(normalizedFile)
  }
  return [...new Set(testFiles)].sort()
}

export function splitTestArguments(args) {
  const bunArguments = []
  const fileFilters = []
  let positionalOnly = false

  for (let index = 0; index < args.length; index += 1) {
    const argument = args[index]
    if (!positionalOnly && argument === '--') {
      positionalOnly = true
      continue
    }
    if (!positionalOnly && argument.startsWith('-')) {
      bunArguments.push(argument)
      if (
        argument === '--shard' &&
        index + 1 < args.length &&
        shardValuePattern.test(args[index + 1])
      ) {
        bunArguments.push(args[index + 1])
        index += 1
      } else if (
        flagsWithSeparateValues.has(argument) &&
        index + 1 < args.length
      ) {
        bunArguments.push(args[index + 1])
        index += 1
      } else if (
        optionalValueFlags.has(argument) &&
        index + 1 < args.length &&
        !args[index + 1].startsWith('-') &&
        optionalValueFlags.get(argument)(args[index + 1])
      ) {
        bunArguments.push(args[index + 1])
        index += 1
      }
      continue
    }
    fileFilters.push(argument)
  }

  return { bunArguments, fileFilters }
}

function matchesFileFilter(file, filter, root) {
  const normalizedFile = normalizePath(file)
  const normalizedFilter = normalizePath(filter).replace(/^\.\//, '')
  const relativeFilter = path.isAbsolute(filter)
    ? normalizePath(path.relative(root, filter))
    : normalizedFilter

  if (/[*?[\]{}]/.test(relativeFilter)) {
    return new Bun.Glob(relativeFilter).match(normalizedFile)
  }
  return normalizedFile.includes(relativeFilter)
}

export function selectTestFiles(files, filters, root = projectRoot) {
  if (filters.length === 0) return files
  return files.filter((file) =>
    filters.some((filter) => matchesFileFilter(file, filter, root))
  )
}

function parseShardValue(value) {
  const match = shardValuePattern.exec(value)
  if (!match) throw new Error(`Invalid shard value: ${value}`)

  const index = Number(match[1])
  const total = Number(match[2])
  if (index < 1 || total < 1 || index > total) {
    throw new Error(`Invalid shard value: ${value}`)
  }
  return { index, total }
}

export function extractShardArgument(args) {
  const bunArguments = []
  let shard

  for (let index = 0; index < args.length; index += 1) {
    const argument = args[index]
    if (argument === '--shard') {
      if (index + 1 >= args.length) throw new Error('--shard requires a value')
      shard = parseShardValue(args[index + 1])
      index += 1
      continue
    }
    if (argument.startsWith('--shard=')) {
      shard = parseShardValue(argument.slice('--shard='.length))
      continue
    }
    bunArguments.push(argument)
  }

  return { bunArguments, shard }
}

export function applyTestShard(files, shard) {
  if (!shard) return files
  return files.filter((_, index) => index % shard.total === shard.index - 1)
}

export function usesIsolatedEnvironment(file, source) {
  return (
    file.endsWith('.tsx') ||
    source.includes('createReactTestEnvironment') ||
    source.includes("from 'jsdom'") ||
    source.includes('from "jsdom"') ||
    globalMutationPattern.test(source)
  )
}

async function partitionTestFiles(files) {
  const sharedFiles = []
  const isolatedFiles = []

  for (const file of files) {
    const source = await Bun.file(path.join(projectRoot, file)).text()
    if (usesIsolatedEnvironment(file, source)) isolatedFiles.push(file)
    else sharedFiles.push(file)
  }
  return { sharedFiles, isolatedFiles }
}

function writeChildOutput(result) {
  if (result.stdout.length > 0) process.stdout.write(result.stdout)
  if (result.stderr.length > 0) process.stderr.write(result.stderr)
}

function validateMaxConcurrencyValue(value) {
  if (value === undefined) {
    throw new Error('--max-concurrency requires a numeric value')
  }
  if (!maxConcurrencyValuePattern.test(value)) {
    throw new Error(`Invalid --max-concurrency value: ${value}`)
  }
}

export function buildTestCommand(files, bunArguments) {
  const forwardedArguments = []
  for (let index = 0; index < bunArguments.length; index += 1) {
    const argument = bunArguments[index]
    if (argument === '--max-concurrency') {
      validateMaxConcurrencyValue(bunArguments[index + 1])
      index += 1
      continue
    }
    if (argument.startsWith('--max-concurrency=')) {
      validateMaxConcurrencyValue(argument.slice('--max-concurrency='.length))
      continue
    }
    forwardedArguments.push(argument)
  }

  return [
    process.execPath,
    'test',
    ...forwardedArguments,
    '--max-concurrency=1',
    ...files,
  ]
}

function runTestFiles(files, label, bunArguments) {
  if (files.length === 0) return 'skipped'

  console.log(`\n==> ${label} (${files.length} file${files.length === 1 ? '' : 's'})`)
  const result = Bun.spawnSync({
    cmd: buildTestCommand(files, bunArguments),
    cwd: projectRoot,
    env: process.env,
    stdout: 'pipe',
    stderr: 'pipe',
  })
  writeChildOutput(result)
  if (result.exitCode === 0) return 'passed'

  const output = `${textDecoder.decode(result.stdout)}\n${textDecoder.decode(result.stderr)}`
  if (output.includes('matched 0 tests')) return 'no-tests'
  process.exit(result.exitCode)
}

async function main() {
  const parsedArguments = splitTestArguments(process.argv.slice(2))
  const shardArguments = extractShardArgument(parsedArguments.bunArguments)
  const bunArguments = shardArguments.bunArguments
  const discoveredFiles = await discoverTestFiles()
  const filteredFiles = selectTestFiles(discoveredFiles, parsedArguments.fileFilters)
  const selectedFiles = applyTestShard(filteredFiles, shardArguments.shard)
  const passWithNoTests = bunArguments.includes('--pass-with-no-tests')

  if (shardArguments.shard) {
    console.log(
      `--shard=${shardArguments.shard.index}/${shardArguments.shard.total}: running ${selectedFiles.length}/${filteredFiles.length} test files`
    )
  }

  if (selectedFiles.length === 0) {
    console.error('No test files matched the supplied filters.')
    process.exit(passWithNoTests ? 0 : 1)
  }

  const { sharedFiles, isolatedFiles } = await partitionTestFiles(selectedFiles)
  let passedRunCount = 0
  if (runTestFiles(sharedFiles, 'Shared-state-safe tests', bunArguments) === 'passed') {
    passedRunCount += 1
  }
  for (const file of isolatedFiles) {
    if (
      runTestFiles([file], `Isolated browser test: ${file}`, bunArguments) ===
      'passed'
    ) {
      passedRunCount += 1
    }
  }

  if (passedRunCount === 0 && !passWithNoTests) process.exit(1)
}

if (import.meta.main) await main()
